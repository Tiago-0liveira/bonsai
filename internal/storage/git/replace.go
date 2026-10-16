package git

import "time"

// retryRename bounds retries so a reader briefly holding a Windows file handle
// does not fail an atomic write, while permanent permission errors still return.
func retryRename(oldpath, newpath string, rename func(string, string) error, retryable func(error) bool, now func() time.Time, sleep func(time.Duration)) error {
	deadline := now().Add(time.Second)
	delay := 5 * time.Millisecond
	for {
		err := rename(oldpath, newpath)
		if err == nil || !retryable(err) {
			return err
		}
		remaining := deadline.Sub(now())
		if remaining <= 0 {
			return err
		}
		sleep(min(delay, remaining))
		if !now().Before(deadline) {
			return err
		}
		delay = min(2*delay, 50*time.Millisecond)
	}
}
