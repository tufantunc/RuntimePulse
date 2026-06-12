//go:build windows

package daemon

// chmodSocket is a no-op on Windows: there is no 0600 equivalent for an
// AF_UNIX socket file. Access control relies on the user-private ACLs
// of %USERPROFILE%\.runtimepulse (windows-support design spec, §1).
func chmodSocket(string) error { return nil }
