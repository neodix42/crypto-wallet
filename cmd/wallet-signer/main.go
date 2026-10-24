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
	"localwallet/internal/signer"
	"localwallet/internal/strictjson"
	"localwallet/internal/uiprompt"
	"localwallet/internal/vault"
	"os"
	"syscall"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(buildinfo.Version)
		return
	}
	syscall.Umask(0077)
	dir := flag.String("data", "", "private profile directory")
	environment := flag.String("environment", "mainnet", "fixed profile environment")
	flag.Parse()
	if *environment != "testnet" && *environment != "mainnet" {
		os.Exit(2)
	}
	if isolation.Install() != nil {
		os.Exit(3)
	}
	files, e := vault.NewFiles(*dir)
	if e != nil {
		os.Exit(5)
	}
	defer files.Close()
	engine := signer.New(files, &uiprompt.UI{}, chains.All())
	engine.Environment = *environment
	defer engine.Lock()
	c := ipc.Channel{R: os.Stdin, W: os.Stdout}
	serve(&c, engine, *environment)
}

// serve serializes engine operations. During an operation the UI exchange owns
// the next input frame, which must be that prompt's exact one-use reply.
func serve(c *ipc.Channel, engine *signer.Engine, environment string) {
	for {
		m, err := c.Read()
		if err != nil || m.Error != "" || c.Accept(m) != nil {
			return
		}
		var promptError error
		if ui, ok := engine.UI.(*uiprompt.UI); ok {
			ui.Exchange = func(p uiprompt.Prompt) (uiprompt.Reply, error) {
				raw, err := json.Marshal(p)
				if err != nil {
					promptError = uiprompt.ErrProtocol
					return uiprompt.Reply{}, promptError
				}
				defer clear(raw)
				err = c.Write(ipc.Message{Version: 1, ID: m.ID, Session: m.Session, Method: "prompt", Body: raw})
				if err != nil {
					promptError = err
					return uiprompt.Reply{}, err
				}
				reply, err := c.Read()
				if err != nil || reply.ID != m.ID || reply.Session != m.Session || reply.Method != "promptReply" || reply.Error != "" {
					promptError = uiprompt.ErrProtocol
					return uiprompt.Reply{}, promptError
				}
				defer clear(reply.Body)
				decoded, err := uiprompt.DecodeReply(reply.Body, p)
				if err != nil {
					promptError = err
				}
				return decoded, err
			}
		}
		err = respond(c, m, func() (json.RawMessage, error) {
			body, err := handle(engine, m, environment)
			if err != nil && promptError == nil {
				notifyFailure(engine.UI, err)
			}
			if promptError != nil {
				return nil, promptError
			}
			return body, err
		})
		if ui, ok := engine.UI.(*uiprompt.UI); ok {
			ui.Exchange = nil
		}
		clear(m.Body)
		if err != nil || promptError != nil {
			return
		}
	}
}

func respond(c *ipc.Channel, m ipc.Message, run func() (json.RawMessage, error)) error {
	body, err := run()
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
	case "unlock", "lock", "snapshot", "settings", "scanQR", "confirmRecovery", "password", "pending":
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
		if err == nil {
			out, err = e.SnapshotPage("")
		}
	case "unlock":
		err = e.Unlock()
		if err == nil && e.Snapshot().Environment != environment {
			e.Lock()
			err = signer.ErrDenied
		}
		if err == nil {
			out, err = e.SnapshotPage("")
		}
	case "lock":
		e.Lock()
		if err == nil {
			out, err = e.SnapshotPage("")
		}
	case "snapshot":
		out, err = e.SnapshotPage("")
	case "accountPage":
		var b struct {
			Cursor strictjson.Text `json:"cursor"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			out, err = e.SnapshotPage(string(b.Cursor))
		}
	case "account":
		var b struct {
			AccountID strictjson.Text `json:"accountId"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			out, err = e.PublicAccount(string(b.AccountID))
		}
	case "journalPage":
		var b struct {
			AccountID strictjson.Text `json:"accountId"`
			Asset     strictjson.Text `json:"asset"`
			Cursor    strictjson.Text `json:"cursor"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			out, err = e.JournalPage(string(b.AccountID), string(b.Asset), string(b.Cursor))
		}
	case "operation":
		var b struct {
			ID strictjson.Text `json:"id"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			out, err = e.Operation(string(b.ID))
		}
	case "addAccount":
		var b struct {
			Index uint32 `json:"index"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			err = e.AddAccount(b.Index)
		}
		if err == nil {
			out, err = e.SnapshotPage("")
		}
	case "cancelPrepared":
		var b struct {
			ID string `json:"id"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			err = e.CancelPrepared(b.ID)
		}
		if err == nil {
			out, err = e.SnapshotPage("")
		}
	case "discoverAccounts":
		var b struct {
			Start uint32 `json:"start"`
			End   uint32 `json:"end"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			err = e.DiscoverAccounts(b.Start, b.End)
		}
		if err == nil {
			out, err = e.SnapshotPage("")
		}
	case "hideAccount":
		var b struct {
			AccountID string `json:"accountId"`
			Hidden    bool   `json:"hidden"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			err = e.HideAccount(b.AccountID, b.Hidden)
		}
		if err == nil {
			out, err = e.SnapshotPage("")
		}
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
		if err == nil {
			out, err = e.SnapshotPage("")
		}
	case "settings":
		out = e.Settings()
	case "associate":
		var a model.Account
		if err = signer.DecodeBody(m.Body, &a); err == nil {
			err = e.Associate(a)
		}
		if err == nil {
			out, err = e.SnapshotPage("")
		}
	case "configure":
		var b struct {
			Network string `json:"network"`
		}
		if err = signer.DecodeBody(m.Body, &b); err == nil {
			err = e.Configure(b.Network)
		}
		if err == nil {
			out, err = e.SnapshotPage("")
		}
	case "scanQR":
		out, err = e.ScanQR()
	case "confirmRecovery":
		err = e.ConfirmRecovery()
		if err == nil {
			out, err = e.SnapshotPage("")
		}
	case "password":
		err = e.ChangePassword()
		if err == nil {
			out, err = e.SnapshotPage("")
		}
	case "prepare":
		var p model.Plan
		if err = signer.DecodeBody(m.Body, &p); err == nil {
			err = e.Prepare(p)
		}
		if err == nil {
			out, err = e.SnapshotPage("")
		}
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
		if err == nil {
			out, err = e.SnapshotPage("")
		}
	case "pending":
		out = e.Pending()
	default:
		err = signer.ErrDenied
	}
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(out)
	if err != nil || len(b) > 768<<10 {
		return nil, signer.ErrDenied
	}
	return b, nil
}
