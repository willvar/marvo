package handler

import "testing"

func TestValidAttachmentFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{name: "plain", filename: "image.png", want: true},
		{name: "spaces", filename: "my image.png", want: true},
		{name: "unicode", filename: "截图.png", want: true},
		{name: "empty", filename: "", want: false},
		{name: "parent path", filename: "../image.png", want: false},
		{name: "nested path", filename: "assets/image.png", want: false},
		{name: "windows path", filename: `assets\image.png`, want: false},
		{name: "parent marker", filename: "image..png", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validAttachmentFilename(tt.filename); got != tt.want {
				t.Fatalf("validAttachmentFilename(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestAttachmentURLEscapesPathSegments(t *testing.T) {
	got := attachmentURL("中文 note", "my image #1.png")
	want := "/api/notes/%E4%B8%AD%E6%96%87%20note/assets/my%20image%20%231.png"
	if got != want {
		t.Fatalf("attachmentURL() = %q, want %q", got, want)
	}
}
