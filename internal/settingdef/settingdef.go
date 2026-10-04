// Package settingdef is the one description of an operator-configurable setting
// that every pluggable family (model providers, search engines) declares so the
// config store and the admin UI can list, mask, and edit them the same way.
package settingdef

// Def describes one configurable setting.
type Def struct {
	Key         string
	Label       string
	Type        string
	Description string
	Secret      bool
}
