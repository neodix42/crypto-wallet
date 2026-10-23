package supervisor

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Only these test subprocesses receive helper selectors. The real Child starts
// the manifest-verified public peer with its normal sanitized environment.
func TestOwnedChildDiesOnAbruptParentProcessDeath(t *testing.T) {
	const selector = "LOCALWALLET_PARENTDEATH_TEST"
	switch os.Getenv(selector) {
	case "owner":
		c := &Child{Path: os.Getenv("LOCALWALLET_PARENTDEATH_PEER"), Data: os.Getenv("LOCALWALLET_PARENTDEATH_DATA"), Environment: "testnet"}
		defer c.Kill()
		if err := c.Call("snapshot", map[string]string{"mode": "none"}, nil); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("READY %d\n", c.cmd.Process.Pid)
		var check [1]byte
		if _, err := io.ReadFull(os.Stdin, check[:]); err != nil {
			t.Fatal(err)
		}
		if err := c.Call("snapshot", map[string]string{"mode": "none"}, nil); err != nil {
			t.Fatal("owned peer died while parent remained alive", err)
		}
		fmt.Println("ALIVE")
		_, _ = io.ReadFull(os.Stdin, check[:]) // controller kills this process abruptly
		return
	case "controller":
		// Isolate subreaper state from the aggregate Go test process, and adopt
		// this one controlled grandchild so even a failing assertion can reap it.
		if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		owner := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOwnedChildDiesOnAbruptParentProcessDeath$")
		owner.Env = append(os.Environ(), selector+"=owner")
		output, err := owner.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		input, err := owner.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		owner.Stderr = os.Stderr
		if err = owner.Start(); err != nil {
			t.Fatal(err)
		}
		pid := 0
		reaped := false
		defer func() {
			_ = input.Close()
			_ = owner.Process.Kill()
			_ = owner.Wait()
			if pid > 0 && !reaped {
				_ = syscall.Kill(pid, syscall.SIGKILL)
				var status syscall.WaitStatus
				_, _ = syscall.Wait4(pid, &status, 0, nil)
			}
		}()
		reader := bufio.NewScanner(output)
		if !reader.Scan() {
			t.Fatal("owner did not announce its actual Child", reader.Err())
		}
		fields := strings.Fields(reader.Text())
		if len(fields) != 2 || fields[0] != "READY" {
			t.Fatal("invalid public helper greeting", reader.Text())
		}
		pid, err = strconv.Atoi(fields[1])
		if err != nil || pid <= 1 {
			t.Fatal("invalid controlled peer PID")
		}
		time.Sleep(150 * time.Millisecond)
		if err = owner.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatal("owner exited prematurely", err)
		}
		if _, err = input.Write([]byte{1}); err != nil {
			t.Fatal(err)
		}
		if !reader.Scan() || reader.Text() != "ALIVE" {
			t.Fatal("actual Child did not remain usable with live parent", reader.Text(), reader.Err())
		}
		if err = owner.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		if err = owner.Wait(); err == nil {
			t.Fatal("controlled parent was not abruptly terminated")
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			var status syscall.WaitStatus
			waited, waitErr := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
			if waitErr != nil {
				t.Fatal("cannot independently reap controlled signer", waitErr)
			}
			if waited == pid {
				reaped = true
				if !status.Signaled() || status.Signal() != syscall.SIGKILL {
					t.Fatal("owned peer exited without required parent-death SIGKILL", status)
				}
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("actual Child survived abrupt parent-process death")
	}
	fixture := syntheticChild(t)
	// EOF alone must not pass the test: this public peer deliberately remains
	// alive when its parent's private input pipe closes. Only SIGKILL suffices.
	peer := strings.Replace(activityPeer, "if len(h)!=4:break", "if len(h)!=4:time.sleep(60);continue", 1)
	if peer == activityPeer {
		t.Fatal("public peer EOF behavior not replaced")
	}
	if err := os.WriteFile(fixture.Path, []byte(peer), 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(peer))
	manifest, _ := json.Marshal(map[string]string{filepath.Base(fixture.Path): hex.EncodeToString(sum[:])})
	if err := os.WriteFile(filepath.Join(filepath.Dir(fixture.Path), "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	controller := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOwnedChildDiesOnAbruptParentProcessDeath$")
	controller.Env = append(os.Environ(), selector+"=controller", "LOCALWALLET_PARENTDEATH_PEER="+fixture.Path, "LOCALWALLET_PARENTDEATH_DATA="+fixture.Data)
	if out, err := controller.CombinedOutput(); err != nil {
		t.Fatalf("actual parent-death subprocess: %v %s", err, out)
	}
}
