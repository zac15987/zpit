//go:build !windows

package notify

// sendToast is a no-op on non-Windows platforms.
func sendToast(_, _ string) error {
	return nil
}
