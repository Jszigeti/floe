package icloud

/*
#include <sys/resource.h>
*/
import "C"

import "fmt"

const (
	datalessOff = int(C.IOPOL_MATERIALIZE_DATALESS_FILES_OFF)
	datalessOn  = int(C.IOPOL_MATERIALIZE_DATALESS_FILES_ON)
)

// launchd starts agents with downloads of evicted iCloud files off; children inherit this process's policy.
func AllowDatalessDownloads() error {
	return setDatalessPolicy(datalessOn)
}

func setDatalessPolicy(policy int) error {
	if rc, err := C.setiopolicy_np(C.IOPOL_TYPE_VFS_MATERIALIZE_DATALESS_FILES, C.IOPOL_SCOPE_PROCESS, C.int(policy)); rc != 0 {
		return fmt.Errorf("setiopolicy_np: %v", err)
	}
	return nil
}

func datalessPolicy() int {
	return int(C.getiopolicy_np(C.IOPOL_TYPE_VFS_MATERIALIZE_DATALESS_FILES, C.IOPOL_SCOPE_PROCESS))
}
