package agent

import (
	"os"
	"runtime"

	"github.com/cachebag/relay/internal/store"
	"github.com/shirou/gopsutil/v4/mem"
)

// Collects and returns system information such as Hostname, OS,
// CPU Architecture, CPU Count, and System Memory.
// runtime.GOOS can either be "linux", "darwin", or "windows".
// runtime.GOARCH can be "amd64" or "arm64".
func Collect() (store.Node, error) {
	host, err := os.Hostname()
	if err != nil {
		return store.Node{}, err
	}
	vm, err := mem.VirtualMemory()
	if err != nil {
		return store.Node{}, err
	}
	return store.Node{
		ID:       store.NewID(),
		Hostname: host,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		CPUs:     runtime.NumCPU(),
		MemBytes: int64(vm.Total),
	}, nil
}
