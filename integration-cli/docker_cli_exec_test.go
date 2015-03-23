package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExec(t *testing.T) {
	runCmd := exec.Command(dockerBinary, "run", "-d", "--name", "testing", "busybox", "sh", "-c", "echo test > /tmp/file && sleep 100")
	if out, _, _, err := runCommandWithStdoutStderr(runCmd); err != nil {
		t.Fatal(out, err)
	}

	execCmd := exec.Command(dockerBinary, "exec", "testing", "cat", "/tmp/file")
	out, _, err := runCommandWithOutput(execCmd)
	if err != nil {
		t.Fatal(out, err)
	}

	out = strings.Trim(out, "\r\n")

	if expected := "test"; out != expected {
		t.Errorf("container exec should've printed %q but printed %q", expected, out)
	}

	deleteAllContainers()

	logDone("exec - basic test")
}

func TestExecInteractiveStdinClose(t *testing.T) {
	defer deleteAllContainers()
	out, _, err := runCommandWithOutput(exec.Command(dockerBinary, "run", "-itd", "busybox", "/bin/cat"))
	if err != nil {
		t.Fatal(err)
	}

	contId := strings.TrimSpace(out)

	returnchan := make(chan struct{})

	go func() {
		var err error
		cmd := exec.Command(dockerBinary, "exec", "-i", contId, "/bin/ls", "/")
		cmd.Stdin = os.Stdin
		if err != nil {
			t.Fatal(err)
		}

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(err, out)
		}

		if string(out) == "" {
			t.Fatalf("Output was empty, likely blocked by standard input")
		}

		returnchan <- struct{}{}
	}()

	select {
	case <-returnchan:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out running docker exec")
	}

	logDone("exec - interactive mode closes stdin after execution")
}

func TestExecInteractive(t *testing.T) {
	runCmd := exec.Command(dockerBinary, "run", "-d", "--name", "testing", "busybox", "sh", "-c", "echo test > /tmp/file && sleep 100")
	if out, _, _, err := runCommandWithStdoutStderr(runCmd); err != nil {
		t.Fatal(out, err)
	}

	execCmd := exec.Command(dockerBinary, "exec", "-i", "testing", "sh")
	stdin, err := execCmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := execCmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	if err := execCmd.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Write([]byte("cat /tmp/file\n")); err != nil {
		t.Fatal(err)
	}

	r := bufio.NewReader(stdout)
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	line = strings.TrimSpace(line)
	if line != "test" {
		t.Fatalf("Output should be 'test', got '%q'", line)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	finish := make(chan struct{})
	go func() {
		if err := execCmd.Wait(); err != nil {
			t.Fatal(err)
		}
		close(finish)
	}()
	select {
	case <-finish:
	case <-time.After(1 * time.Second):
		t.Fatal("docker exec failed to exit on stdin close")
	}

	deleteAllContainers()

	logDone("exec - Interactive test")
}

func TestExecAfterContainerRestart(t *testing.T) {
	runCmd := exec.Command(dockerBinary, "run", "-d", "busybox", "top")
	out, _, err := runCommandWithOutput(runCmd)
	if err != nil {
		t.Fatal(out, err)
	}

	cleanedContainerID := stripTrailingCharacters(out)

	runCmd = exec.Command(dockerBinary, "restart", cleanedContainerID)
	if out, _, err = runCommandWithOutput(runCmd); err != nil {
		t.Fatal(out, err)
	}

	runCmd = exec.Command(dockerBinary, "exec", cleanedContainerID, "echo", "hello")
	out, _, err = runCommandWithOutput(runCmd)
	if err != nil {
		t.Fatal(out, err)
	}

	outStr := strings.TrimSpace(out)
	if outStr != "hello" {
		t.Errorf("container should've printed hello, instead printed %q", outStr)
	}

	deleteAllContainers()

	logDone("exec - exec running container after container restart")
}

func TestExecAfterDaemonRestart(t *testing.T) {
	d := NewDaemon(t)
	if err := d.StartWithBusybox(); err != nil {
		t.Fatalf("Could not start daemon with busybox: %v", err)
	}
	defer d.Stop()

	if out, err := d.Cmd("run", "-d", "--name", "top", "-p", "80", "busybox:latest", "top"); err != nil {
		t.Fatalf("Could not run top: err=%v\n%s", err, out)
	}

	if err := d.Restart(); err != nil {
		t.Fatalf("Could not restart daemon: %v", err)
	}

	if out, err := d.Cmd("start", "top"); err != nil {
		t.Fatalf("Could not start top after daemon restart: err=%v\n%s", err, out)
	}

	out, err := d.Cmd("exec", "top", "echo", "hello")
	if err != nil {
		t.Fatalf("Could not exec on container top: err=%v\n%s", err, out)
	}

	outStr := strings.TrimSpace(string(out))
	if outStr != "hello" {
		t.Errorf("container should've printed hello, instead printed %q", outStr)
	}

	logDone("exec - exec running container after daemon restart")
}

// Regresssion test for #9155, #9044
func TestExecEnv(t *testing.T) {
	defer deleteAllContainers()

	runCmd := exec.Command(dockerBinary, "run",
		"-e", "LALA=value1",
		"-e", "LALA=value2",
		"-d", "--name", "testing", "busybox", "top")
	if out, _, _, err := runCommandWithStdoutStderr(runCmd); err != nil {
		t.Fatal(out, err)
	}

	execCmd := exec.Command(dockerBinary, "exec", "testing", "env")
	out, _, err := runCommandWithOutput(execCmd)
	if err != nil {
		t.Fatal(out, err)
	}

	if strings.Contains(out, "LALA=value1") ||
		!strings.Contains(out, "LALA=value2") ||
		!strings.Contains(out, "HOME=/root") {
		t.Errorf("exec env(%q), expect %q, %q", out, "LALA=value2", "HOME=/root")
	}

	logDone("exec - exec inherits correct env")
}

func TestExecExitStatus(t *testing.T) {
	runCmd := exec.Command(dockerBinary, "run", "-d", "--name", "top", "busybox", "top")
	if out, _, _, err := runCommandWithStdoutStderr(runCmd); err != nil {
		t.Fatal(out, err)
	}

	// Test normal (non-detached) case first
	cmd := exec.Command(dockerBinary, "exec", "top", "sh", "-c", "exit 23")
	ec, _ := runCommand(cmd)

	if ec != 23 {
		t.Fatalf("Should have had an ExitCode of 23, not: %d", ec)
	}

	logDone("exec - exec non-zero ExitStatus")
}

func TestExecPausedContainer(t *testing.T) {

	defer deleteAllContainers()
	defer unpauseAllContainers()

	runCmd := exec.Command(dockerBinary, "run", "-d", "--name", "testing", "busybox", "top")
	out, _, err := runCommandWithOutput(runCmd)
	if err != nil {
		t.Fatal(out, err)
	}

	ContainerID := stripTrailingCharacters(out)

	pausedCmd := exec.Command(dockerBinary, "pause", "testing")
	out, _, _, err = runCommandWithStdoutStderr(pausedCmd)
	if err != nil {
		t.Fatal(out, err)
	}

	execCmd := exec.Command(dockerBinary, "exec", "-i", "-t", ContainerID, "echo", "hello")
	out, _, err = runCommandWithOutput(execCmd)
	if err == nil {
		t.Fatal("container should fail to exec new command if it is paused")
	}

	expected := ContainerID + " is paused, unpause the container before exec"
	if !strings.Contains(out, expected) {
		t.Fatal("container should not exec new command if it is paused")
	}

	logDone("exec - exec should not exec a pause container")
}

// regression test for #9476
func TestExecTtyCloseStdin(t *testing.T) {
	defer deleteAllContainers()

	cmd := exec.Command(dockerBinary, "run", "-d", "-it", "--name", "exec_tty_stdin", "busybox")
	if out, _, err := runCommandWithOutput(cmd); err != nil {
		t.Fatal(out, err)
	}

	cmd = exec.Command(dockerBinary, "exec", "-i", "exec_tty_stdin", "cat")
	stdinRw, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}

	stdinRw.Write([]byte("test"))
	stdinRw.Close()

	if out, _, err := runCommandWithOutput(cmd); err != nil {
		t.Fatal(out, err)
	}

	cmd = exec.Command(dockerBinary, "top", "exec_tty_stdin")
	out, _, err := runCommandWithOutput(cmd)
	if err != nil {
		t.Fatal(out, err)
	}

	outArr := strings.Split(out, "\n")
	if len(outArr) > 3 || strings.Contains(out, "nsenter-exec") {
		// This is the really bad part
		if out, _, err := runCommandWithOutput(exec.Command(dockerBinary, "rm", "-f", "exec_tty_stdin")); err != nil {
			t.Fatal(out, err)
		}

		t.Fatalf("exec process left running\n\t %s", out)
	}

	logDone("exec - stdin is closed properly with tty enabled")
}

func TestExecTtyWithoutStdin(t *testing.T) {
	defer deleteAllContainers()

	cmd := exec.Command(dockerBinary, "run", "-d", "-ti", "busybox")
	out, _, err := runCommandWithOutput(cmd)
	if err != nil {
		t.Fatalf("failed to start container: %v (%v)", out, err)
	}

	id := strings.TrimSpace(out)
	if err := waitRun(id); err != nil {
		t.Fatal(err)
	}

	defer func() {
		cmd := exec.Command(dockerBinary, "kill", id)
		if out, _, err := runCommandWithOutput(cmd); err != nil {
			t.Fatalf("failed to kill container: %v (%v)", out, err)
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)

		cmd := exec.Command(dockerBinary, "exec", "-ti", id, "true")
		if _, err := cmd.StdinPipe(); err != nil {
			t.Fatal(err)
		}

		expected := "cannot enable tty mode"
		if out, _, err := runCommandWithOutput(cmd); err == nil {
			t.Fatal("exec should have failed")
		} else if !strings.Contains(out, expected) {
			t.Fatalf("exec failed with error %q: expected %q", out, expected)
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("exec is running but should have failed")
	}

	logDone("exec - forbid piped stdin to tty enabled container")
}

func TestExecParseError(t *testing.T) {
	defer deleteAllContainers()

	runCmd := exec.Command(dockerBinary, "run", "-d", "--name", "top", "busybox", "top")
	if out, _, err := runCommandWithOutput(runCmd); err != nil {
		t.Fatal(out, err)
	}

	// Test normal (non-detached) case first
	cmd := exec.Command(dockerBinary, "exec", "top")
	if _, stderr, code, err := runCommandWithStdoutStderr(cmd); err == nil || !strings.Contains(stderr, "See '"+dockerBinary+" exec --help'") || code == 0 {
		t.Fatalf("Should have thrown error & point to help: %s", stderr)
	}
	logDone("exec - error on parseExec should point to help")
}

func TestExecStopNotHanging(t *testing.T) {
	defer deleteAllContainers()
	if out, err := exec.Command(dockerBinary, "run", "-d", "--name", "testing", "busybox", "top").CombinedOutput(); err != nil {
		t.Fatal(out, err)
	}

	if err := exec.Command(dockerBinary, "exec", "testing", "top").Start(); err != nil {
		t.Fatal(err)
	}

	wait := make(chan struct{})
	go func() {
		if out, err := exec.Command(dockerBinary, "stop", "testing").CombinedOutput(); err != nil {
			t.Fatal(out, err)
		}
		close(wait)
	}()
	select {
	case <-time.After(3 * time.Second):
		t.Fatal("Container stop timed out")
	case <-wait:
	}
	logDone("exec - container with exec not hanging on stop")
}

func TestExecCgroup(t *testing.T) {
	defer deleteAllContainers()
	var cmd *exec.Cmd

	cmd = exec.Command(dockerBinary, "run", "-d", "--name", "testing", "busybox", "top")
	_, err := runCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}

	cmd = exec.Command(dockerBinary, "exec", "testing", "cat", "/proc/1/cgroup")
	out, _, err := runCommandWithOutput(cmd)
	if err != nil {
		t.Fatal(out, err)
	}
	containerCgroups := sort.StringSlice(strings.Split(string(out), "\n"))

	var wg sync.WaitGroup
	var s sync.Mutex
	execCgroups := []sort.StringSlice{}
	// exec a few times concurrently to get consistent failure
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			cmd = exec.Command(dockerBinary, "exec", "testing", "cat", "/proc/self/cgroup")
			out, _, err := runCommandWithOutput(cmd)
			if err != nil {
				t.Fatal(out, err)
			}
			cg := sort.StringSlice(strings.Split(string(out), "\n"))

			s.Lock()
			execCgroups = append(execCgroups, cg)
			s.Unlock()
			wg.Done()
		}()
	}
	wg.Wait()

	for _, cg := range execCgroups {
		if !reflect.DeepEqual(cg, containerCgroups) {
			fmt.Println("exec cgroups:")
			for _, name := range cg {
				fmt.Printf(" %s\n", name)
			}

			fmt.Println("container cgroups:")
			for _, name := range containerCgroups {
				fmt.Printf(" %s\n", name)
			}
			t.Fatal("cgroups mismatched")
		}
	}

	logDone("exec - exec has the container cgroups")
}
