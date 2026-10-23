package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"localwallet/internal/buildinfo"
	"localwallet/internal/chains"
	"localwallet/internal/ipc"
	"localwallet/internal/isolation"
	"localwallet/internal/model"
	"localwallet/internal/secureui"
	"localwallet/internal/signer"
	"localwallet/internal/vault"
	"os"
	"runtime"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(buildinfo.Version)
		return
	}
	runtime.LockOSThread()
	syscall.Umask(0077)
	dir := flag.String("data", "", "private profile directory")
	environment := flag.String("environment", "testnet", "fixed profile environment")
	flag.Parse()
	if *environment != "testnet" && *environment != "mainnet" {
		os.Exit(2)
	}
	if isolation.Install() != nil {
		os.Exit(3)
	}
	if secureui.Init() != nil {
		os.Exit(4)
	}
	files, e := vault.NewFiles(*dir)
	if e != nil {
		os.Exit(5)
	}
	defer files.Close()
	engine := signer.New(files, secureui.Native{}, chains.All())
	engine.Environment = *environment
	defer engine.Lock()
	c := ipc.Channel{R: os.Stdin, W: os.Stdout}
	serve(&c, engine, *environment)
}

func serve(c *ipc.Channel, engine *signer.Engine, environment string) {
	type incoming struct {
		message ipc.Message
		err     error
	}
	requests := make(chan incoming, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			m, err := c.Read()
			select {
			case requests <- incoming{m, err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	pump := time.NewTicker(20 * time.Millisecond)
	defer pump.Stop()
	for {
		select {
		case <-pump.C:
			secureui.Pump()
		case request := <-requests:
			m := request.message
			if request.err != nil || c.Accept(m) != nil {
				return
			}
			if respond(c, m, func() (json.RawMessage, error) {
				body, err := handle(engine, m, environment)
				if err != nil {
					notifyFailure(engine.UI, err)
				}
				return body, err
			}) != nil {
				return
			}
		}
	}
}

// respond keeps activity on the same bounded private pipe as its request.
// The callback is removed before the final response, including error paths.
func respond(c *ipc.Channel, m ipc.Message, run func() (json.RawMessage, error)) error {
	var activityError error
	secureui.SetActivity(func() {
		if activityError == nil {
			activityError = c.Write(ipc.Message{Version: 1, ID: m.ID, Session: m.Session, Method: "activity", Body: []byte(`{}`)})
		}
	})
	defer secureui.SetActivity(nil)
	body, err := run()
	secureui.SetActivity(nil)
	if activityError != nil {
		return activityError
	}
	response := ipc.Message{Version: 1, ID: m.ID, Session: m.Session, Method: m.Method, Body: body}
	if err != nil {
		response.Body = []byte(`null`)
		response.Error = requestErrorCode(err)
	}
	return c.Write(response)
}
func handle(e *signer.Engine, m ipc.Message, environment string) (json.RawMessage, error) {
	// JSON null does not represent an argument object. The standard decoder
	// otherwise accepts it into a struct without changing its zero values.
	body := bytes.TrimSpace(m.Body)
	if len(body) == 0 || body[0] != '{' {
		return nil, signer.ErrDenied
	}
	var out any
	var err error
	switch m.Method {
	case "unlock", "lock", "snapshot", "settings", "scanQR", "backup", "restore", "password", "pending":
		if signer.DecodeBody(m.Body, &struct{}{}) != nil {
			return nil, signer.ErrDenied
		}
	}
	switch m.Method {
	case "create", "import":
		var b struct {
			Words int `json:"words"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			err = e.Create(environment, b.Words, m.Method == "import")
		}
		out = e.Snapshot()
	case "unlock":
		err = e.Unlock()
		if err == nil && e.Snapshot().Environment != environment {
			e.Lock()
			err = signer.ErrDenied
		}
		out = e.Snapshot()
	case "lock":
		e.Lock()
		out = e.Snapshot()
	case "snapshot":
		out = e.Snapshot()
	case "addAccount":
		var b struct {
			Index uint32 `json:"index"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			err = e.AddAccount(b.Index)
		}
		out = e.Snapshot()
	case "cancelPrepared":
		var b struct {
			ID string `json:"id"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			err = e.CancelPrepared(b.ID)
		}
		out = e.Snapshot()
	case "discoverAccounts":
		var b struct {
			Start uint32 `json:"start"`
			End   uint32 `json:"end"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			err = e.DiscoverAccounts(b.Start, b.End)
		}
		out = e.Snapshot()
	case "hideAccount":
		var b struct {
			AccountID string `json:"accountId"`
			Hidden    bool   `json:"hidden"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			err = e.HideAccount(b.AccountID, b.Hidden)
		}
		out = e.Snapshot()
	case "receive":
		var b struct {
			AccountID string `json:"accountId"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			var png []byte
			png, err = e.Receive(b.AccountID)
			var account model.Account
			for _, a := range e.Snapshot().Accounts {
				if a.ID == b.AccountID {
					account = a
				}
			}
			out = struct {
				PNG     []byte        `json:"png"`
				Account model.Account `json:"account"`
			}{png, account}
		}
	case "copyReceive", "saveQR":
		var b struct {
			AccountID string `json:"accountId"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			if m.Method == "copyReceive" {
				err = e.CopyReceive(b.AccountID)
			} else {
				err = e.SaveQR(b.AccountID)
			}
		}
	case "discovery":
		var b struct {
			AccountID   string `json:"accountId"`
			Birthday    uint32 `json:"birthday"`
			Gap         uint32 `json:"gap"`
			Limit       uint32 `json:"limit"`
			BlockBudget uint32 `json:"blockBudget"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			err = e.Discovery(b.AccountID, b.Birthday, b.Gap, b.Limit, b.BlockBudget)
		}
		out = e.Snapshot()
	case "settings":
		out = e.Settings()
	case "associate":
		var a model.Account
		if err = signer.DecodeBody(m.Body, &a); err == nil {
			err = e.Associate(a)
		}
		out = e.Snapshot()
	case "configure":
		var b struct {
			Network string `json:"network"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			err = e.Configure(b.Network)
		}
		out = e.Snapshot()
	case "scanQR":
		out, err = e.ScanQR()
	case "backup":
		err = e.Backup()
		out = e.Snapshot()
	case "restore":
		err = e.Restore()
		if err == nil && e.Snapshot().Environment != environment {
			e.Lock()
			err = signer.ErrDenied
		}
		out = e.Snapshot()
	case "password":
		err = e.ChangePassword()
		out = e.Snapshot()
	case "prepare":
		var p model.Plan
		if err = signer.DecodeBody(m.Body, &p); err == nil {
			err = e.Prepare(p)
		}
		out = e.Snapshot()
	case "sign":
		var b struct {
			ID string `json:"id"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			out, err = e.Sign(b.ID)
		}
	case "update":
		var b struct {
			ID      string         `json:"id"`
			State   string         `json:"state"`
			Receipt *model.Receipt `json:"receipt"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			err = e.Update(b.ID, b.State, b.Receipt)
		}
		out = e.Snapshot()
	case "pending":
		out = e.Pending()
	default:
		err = signer.ErrDenied
	}
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(out)
	return b, err
}
