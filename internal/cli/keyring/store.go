package keyring

import z "github.com/zalando/go-keyring"

type Tokens struct{ Access, Refresh string }
type Store struct{ Service string }

func (s Store) Save(t Tokens) error {
	if e := z.Set(s.Service, "access", t.Access); e != nil {
		return e
	}
	return z.Set(s.Service, "refresh", t.Refresh)
}
func (s Store) Load() (Tokens, error) {
	a, e := z.Get(s.Service, "access")
	if e != nil {
		return Tokens{}, e
	}
	r, e := z.Get(s.Service, "refresh")
	return Tokens{a, r}, e
}
