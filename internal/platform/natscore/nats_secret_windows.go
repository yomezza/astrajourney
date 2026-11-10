package natscore

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Reject broad built-in grants. Custom/domain group membership still requires
// an administrator's icacls review; do not claim this is a complete ACL audit.
func checkNATSSecretPermissions(path string, _ os.FileInfo) error {
	sd, e := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		return errors.New("cannot inspect NATS password_file DACL")
	}
	acl, _, e := sd.DACL()
	if e != nil || acl == nil {
		return errors.New("NATS password_file requires a restricted DACL")
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if e = windows.GetAce(acl, i, &ace); e != nil {
			return errors.New("cannot inspect NATS password_file ACE")
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return errors.New("NATS password_file has an unsupported DACL ACE; review its access explicitly")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if ace.Mask != 0 && (sid.IsWellKnown(windows.WinWorldSid) || sid.IsWellKnown(windows.WinBuiltinUsersSid) || sid.IsWellKnown(windows.WinAuthenticatedUserSid) || sid.IsWellKnown(windows.WinAnonymousSid)) {
			return errors.New("NATS password_file grants access to a broad Windows group; restrict its DACL")
		}
	}
	return nil
}
