package main

import "testing"

// The live-key guard depends on recognising a laptop. Getting this wrong in
// the permissive direction lets a development machine charge real cards.
func TestIsLocalURL(t *testing.T) {
	local := []string{
		"http://localhost:3000",
		"postgres://shellhaki:secret@localhost:5432/envi",
		"postgres://u@127.0.0.1/envi?sslmode=disable",
		"postgresql:///envi",
		"http://[::1]:8080",
		"http://0.0.0.0:3000",
		"http://app.localhost",
	}
	remote := []string{
		"https://envisecrets.com",
		"https://api.envisecrets.com",
		"postgres://u:p@ep-cool-name.eu-central-1.aws.neon.tech/envi?sslmode=require",
		"postgres://u:p@10.0.0.5:5432/envi",
	}
	for _, u := range local {
		if !isLocalURL(u) {
			t.Errorf("%s was not recognised as local", u)
		}
	}
	for _, u := range remote {
		if isLocalURL(u) {
			t.Errorf("%s was taken for local", u)
		}
	}
}
