//go:build !linux && !darwin && !windows

package keyring

import "errors"

type unsupported struct{}

func (unsupported) Save(Tokens) error {
	return errors.New("OS keychain is not supported; use ENVI_TOKEN")
}
func (unsupported) Load() (Tokens, error) {
	return Tokens{}, errors.New("OS keychain is not supported; use ENVI_TOKEN")
}
func New() Store { return Store{Service: "unsupported"} }
