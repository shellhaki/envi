package mailer

import "time"

// retryDelays between attempts, shared by every sender in this package. A
// flaky connection (a mobile hotspot's DNS blipping, a momentary dropped
// route, a provider's transient 5xx) is common enough to be worth riding out
// with a couple of retries rather than failing the whole send.
var retryDelays = []time.Duration{time.Second, 3 * time.Second}

// withRetry runs send, retrying on error per retryDelays before giving up and
// returning the last error.
func withRetry(send func() error) error {
	var err error
	for attempt := 0; ; attempt++ {
		if err = send(); err == nil {
			return nil
		}
		if attempt >= len(retryDelays) {
			return err
		}
		time.Sleep(retryDelays[attempt])
	}
}
