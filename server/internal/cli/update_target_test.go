package cli

import "testing"

func TestValidateBinaryUpdateTarget(t *testing.T) {
	for _, tt := range []struct {
		name, goos, path string
		blocked          bool
	}{
		{"agent host", "darwin", "/Applications/NexAI Agent Host.app/Contents/MacOS/multica", true},
		{"other bundle", "darwin", "/Applications/Other.APP/Contents/MacOS/cli", true},
		{"cleaned path", "darwin", "/Applications/Host.app/Contents/MacOS/./multica", true},
		{"homebrew standalone", "darwin", "/opt/homebrew/Cellar/multica/1/bin/multica", false},
		{"app in directory name", "darwin", "/tmp/Host.app/build/multica", false},
		{"linux", "linux", "/tmp/Host.app/Contents/MacOS/multica", false},
		{"windows", "windows", "/tmp/Host.app/Contents/MacOS/multica", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateBinaryUpdateTarget(tt.goos, tt.path); (err != nil) != tt.blocked {
				t.Fatalf("error = %v, want blocked = %v", err, tt.blocked)
			}
		})
	}
}
