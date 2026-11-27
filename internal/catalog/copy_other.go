//go:build !darwin

package catalog

import "os"

func cloneLocal(*os.File, string) error { return os.ErrInvalid }
