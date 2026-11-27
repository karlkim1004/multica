//go:build !nex1979_qa_capture

package main

import "github.com/multica-ai/multica/server/internal/daemon"

func configureQATurnCapture(_ *daemon.Config) error { return nil }
