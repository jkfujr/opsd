//go:build !linux

package installation

func PrivateFiles()      {}
func NotifyReady() error { return nil }
