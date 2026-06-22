# Cross-browser Compatibility — NEX-580

## Browser APIs
- STT supports `SpeechRecognition` and `webkitSpeechRecognition`.
- Unsupported STT: mic disabled + visible message.
- TTS supports `speechSynthesis` + `SpeechSynthesisUtterance`.
- Unsupported TTS: speaker toggle disabled.

## Known Limits
- Browser Web Speech support varies. Chrome/WebKit paths are covered; unsupported browsers degrade without blocking text chat.
