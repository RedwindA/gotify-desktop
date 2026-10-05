//go:build !windows && !darwin && !linux

package notify

func New(appID, appName, cacheDir string, appIconPNG []byte) (Notifier, error) {
	return unsupported{}, nil
}
