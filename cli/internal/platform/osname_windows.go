package platform

import (
	"fmt"
	"os"
	"sync"

	"golang.org/x/sys/windows"
)

var OSName = sync.OnceValue(func() string {
	v := windows.RtlGetVersion()
	// Windows 11 still reports 10; build 22000 is where 11 starts
	if v.MajorVersion == 10 && v.BuildNumber >= 22000 {
		return "Windows 11"
	}
	return fmt.Sprintf("Windows %d", v.MajorVersion)
})

func ComputerName() string {
	h, _ := os.Hostname()
	return h
}
