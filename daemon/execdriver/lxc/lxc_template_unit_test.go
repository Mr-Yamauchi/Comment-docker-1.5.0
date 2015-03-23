// +build linux

package lxc

import (
	"bufio"
	"fmt"
	"github.com/docker/docker/daemon/execdriver"
	nativeTemplate "github.com/docker/docker/daemon/execdriver/native/template"
	"github.com/docker/libcontainer/devices"
	"github.com/docker/libcontainer/security/capabilities"
	"github.com/syndtr/gocapability/capability"
	"io/ioutil"
	"math/rand"
	"os"
	"path"
	"strings"
	"testing"
	"time"
)

func TestLXCConfig(t *testing.T) {
	root, err := ioutil.TempDir("", "TestLXCConfig")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	os.MkdirAll(path.Join(root, "containers", "1"), 0777)

	// Memory is allocated randomly for testing
	rand.Seed(time.Now().UTC().UnixNano())
	var (
		memMin = 33554432
		memMax = 536870912
		mem    = memMin + rand.Intn(memMax-memMin)
		cpuMin = 100
		cpuMax = 10000
		cpu    = cpuMin + rand.Intn(cpuMax-cpuMin)
	)

	driver, err := NewDriver(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	command := &execdriver.Command{
		ID: "1",
		Resources: &execdriver.Resources{
			Memory:    int64(mem),
			CpuShares: int64(cpu),
		},
		Network: &execdriver.Network{
			Mtu:       1500,
			Interface: nil,
		},
		AllowedDevices: make([]*devices.Device, 0),
		ProcessConfig:  execdriver.ProcessConfig{},
	}
	p, err := driver.generateLXCConfig(command)
	if err != nil {
		t.Fatal(err)
	}
	grepFile(t, p,
		fmt.Sprintf("lxc.cgroup.memory.limit_in_bytes = %d", mem))

	grepFile(t, p,
		fmt.Sprintf("lxc.cgroup.memory.memsw.limit_in_bytes = %d", mem*2))
}

func TestCustomLxcConfig(t *testing.T) {
	root, err := ioutil.TempDir("", "TestCustomLxcConfig")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	os.MkdirAll(path.Join(root, "containers", "1"), 0777)

	driver, err := NewDriver(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	processConfig := execdriver.ProcessConfig{
		Privileged: false,
	}
	command := &execdriver.Command{
		ID: "1",
		LxcConfig: []string{
			"lxc.utsname = docker",
			"lxc.cgroup.cpuset.cpus = 0,1",
		},
		Network: &execdriver.Network{
			Mtu:       1500,
			Interface: nil,
		},
		ProcessConfig: processConfig,
	}

	p, err := driver.generateLXCConfig(command)
	if err != nil {
		t.Fatal(err)
	}

	grepFile(t, p, "lxc.utsname = docker")
	grepFile(t, p, "lxc.cgroup.cpuset.cpus = 0,1")
}

func grepFile(t *testing.T, path string, pattern string) {
	grepFileWithReverse(t, path, pattern, false)
}

func grepFileWithReverse(t *testing.T, path string, pattern string, inverseGrep bool) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var (
		line string
	)
	err = nil
	for err == nil {
		line, err = r.ReadString('\n')
		if strings.Contains(line, pattern) == true {
			if inverseGrep {
				t.Fatalf("grepFile: pattern \"%s\" found in \"%s\"", pattern, path)
			}
			return
		}
	}
	if inverseGrep {
		return
	}
	t.Fatalf("grepFile: pattern \"%s\" not found in \"%s\"", pattern, path)
}

func TestEscapeFstabSpaces(t *testing.T) {
	var testInputs = map[string]string{
		" ":                      "\\040",
		"":                       "",
		"/double  space":         "/double\\040\\040space",
		"/some long test string": "/some\\040long\\040test\\040string",
		"/var/lib/docker":        "/var/lib/docker",
		" leading":               "\\040leading",
		"trailing ":              "trailing\\040",
	}
	for in, exp := range testInputs {
		if out := escapeFstabSpaces(in); exp != out {
			t.Logf("Expected %s got %s", exp, out)
			t.Fail()
		}
	}
}

func TestIsDirectory(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "TestIsDir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	tempFile, err := ioutil.TempFile(tempDir, "TestIsDirFile")
	if err != nil {
		t.Fatal(err)
	}

	if isDirectory(tempDir) != "dir" {
		t.Logf("Could not identify %s as a directory", tempDir)
		t.Fail()
	}

	if isDirectory(tempFile.Name()) != "file" {
		t.Logf("Could not identify %s as a file", tempFile.Name())
		t.Fail()
	}
}

func TestCustomLxcConfigMounts(t *testing.T) {
	root, err := ioutil.TempDir("", "TestCustomLxcConfig")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	tempDir, err := ioutil.TempDir("", "TestIsDir")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	tempFile, err := ioutil.TempFile(tempDir, "TestIsDirFile")
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(path.Join(root, "containers", "1"), 0777)

	driver, err := NewDriver(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	processConfig := execdriver.ProcessConfig{
		Privileged: false,
	}
	mounts := []execdriver.Mount{
		{
			Source:      tempDir,
			Destination: tempDir,
			Writable:    false,
			Private:     true,
		},
		{
			Source:      tempFile.Name(),
			Destination: tempFile.Name(),
			Writable:    true,
			Private:     true,
		},
	}
	command := &execdriver.Command{
		ID: "1",
		LxcConfig: []string{
			"lxc.utsname = docker",
			"lxc.cgroup.cpuset.cpus = 0,1",
		},
		Network: &execdriver.Network{
			Mtu:       1500,
			Interface: nil,
		},
		Mounts:        mounts,
		ProcessConfig: processConfig,
	}

	p, err := driver.generateLXCConfig(command)
	if err != nil {
		t.Fatal(err)
	}

	grepFile(t, p, "lxc.utsname = docker")
	grepFile(t, p, "lxc.cgroup.cpuset.cpus = 0,1")

	grepFile(t, p, fmt.Sprintf("lxc.mount.entry = %s %s none rbind,ro,create=%s 0 0", tempDir, "/"+tempDir, "dir"))
	grepFile(t, p, fmt.Sprintf("lxc.mount.entry = %s %s none rbind,rw,create=%s 0 0", tempFile.Name(), "/"+tempFile.Name(), "file"))
}

func TestCustomLxcConfigMisc(t *testing.T) {
	root, err := ioutil.TempDir("", "TestCustomLxcConfig")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	os.MkdirAll(path.Join(root, "containers", "1"), 0777)
	driver, err := NewDriver(root, "", true)

	if err != nil {
		t.Fatal(err)
	}
	processConfig := execdriver.ProcessConfig{
		Privileged: false,
	}

	processConfig.Env = []string{"HOSTNAME=testhost"}
	command := &execdriver.Command{
		ID: "1",
		LxcConfig: []string{
			"lxc.cgroup.cpuset.cpus = 0,1",
		},
		Network: &execdriver.Network{
			Mtu: 1500,
			Interface: &execdriver.NetworkInterface{
				Gateway:     "10.10.10.1",
				IPAddress:   "10.10.10.10",
				IPPrefixLen: 24,
				Bridge:      "docker0",
			},
		},
		ProcessConfig:   processConfig,
		CapAdd:          []string{"net_admin", "syslog"},
		CapDrop:         []string{"kill", "mknod"},
		AppArmorProfile: "lxc-container-default-with-nesting",
	}

	p, err := driver.generateLXCConfig(command)
	if err != nil {
		t.Fatal(err)
	}
	// network
	grepFile(t, p, "lxc.network.type = veth")
	grepFile(t, p, "lxc.network.link = docker0")
	grepFile(t, p, "lxc.network.name = eth0")
	grepFile(t, p, "lxc.network.ipv4 = 10.10.10.10/24")
	grepFile(t, p, "lxc.network.ipv4.gateway = 10.10.10.1")
	grepFile(t, p, "lxc.network.flags = up")
	grepFile(t, p, "lxc.aa_profile = lxc-container-default-with-nesting")
	// hostname
	grepFile(t, p, "lxc.utsname = testhost")
	grepFile(t, p, "lxc.cgroup.cpuset.cpus = 0,1")
	container := nativeTemplate.New()
	for _, cap := range container.Capabilities {
		realCap := capabilities.GetCapability(cap)
		numCap := fmt.Sprintf("%d", realCap.Value)
		if cap != "MKNOD" && cap != "KILL" {
			grepFile(t, p, fmt.Sprintf("lxc.cap.keep = %s", numCap))
		}
	}

	grepFileWithReverse(t, p, fmt.Sprintf("lxc.cap.keep = %d", capability.CAP_KILL), true)
	grepFileWithReverse(t, p, fmt.Sprintf("lxc.cap.keep = %d", capability.CAP_MKNOD), true)
}

func TestCustomLxcConfigMiscOverride(t *testing.T) {
	root, err := ioutil.TempDir("", "TestCustomLxcConfig")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	os.MkdirAll(path.Join(root, "containers", "1"), 0777)
	driver, err := NewDriver(root, "", false)
	if err != nil {
		t.Fatal(err)
	}
	processConfig := execdriver.ProcessConfig{
		Privileged: false,
	}

	processConfig.Env = []string{"HOSTNAME=testhost"}
	command := &execdriver.Command{
		ID: "1",
		LxcConfig: []string{
			"lxc.cgroup.cpuset.cpus = 0,1",
			"lxc.network.ipv4 = 172.0.0.1",
		},
		Network: &execdriver.Network{
			Mtu: 1500,
			Interface: &execdriver.NetworkInterface{
				Gateway:     "10.10.10.1",
				IPAddress:   "10.10.10.10",
				IPPrefixLen: 24,
				Bridge:      "docker0",
			},
		},
		ProcessConfig: processConfig,
		CapAdd:        []string{"NET_ADMIN", "SYSLOG"},
		CapDrop:       []string{"KILL", "MKNOD"},
	}

	p, err := driver.generateLXCConfig(command)
	if err != nil {
		t.Fatal(err)
	}
	// network
	grepFile(t, p, "lxc.network.type = veth")
	grepFile(t, p, "lxc.network.link = docker0")
	grepFile(t, p, "lxc.network.name = eth0")
	grepFile(t, p, "lxc.network.ipv4 = 172.0.0.1")
	grepFile(t, p, "lxc.network.ipv4.gateway = 10.10.10.1")
	grepFile(t, p, "lxc.network.flags = up")

	// hostname
	grepFile(t, p, "lxc.utsname = testhost")
	grepFile(t, p, "lxc.cgroup.cpuset.cpus = 0,1")
	container := nativeTemplate.New()
	for _, cap := range container.Capabilities {
		realCap := capabilities.GetCapability(cap)
		numCap := fmt.Sprintf("%d", realCap.Value)
		if cap != "MKNOD" && cap != "KILL" {
			grepFile(t, p, fmt.Sprintf("lxc.cap.keep = %s", numCap))
		}
	}
	grepFileWithReverse(t, p, fmt.Sprintf("lxc.cap.keep = %d", capability.CAP_KILL), true)
	grepFileWithReverse(t, p, fmt.Sprintf("lxc.cap.keep = %d", capability.CAP_MKNOD), true)
}
