package nmconfig

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/ini.v1"
)

type File struct {
	f *ini.File
}

func Parse(b []byte) (*File, error) {
	if len(b) == 0 {
		return NewEmpty(), nil
	}

	opt := ini.LoadOptions{
		// NetworkManager keyfiles are INI-ish; be permissive.
		SkipUnrecognizableLines: true,
		AllowBooleanKeys:        true,
		// NM keyfiles use ';' as a list delimiter for many values (e.g. allowed-ips).
		// Treating ';' as an inline comment would truncate values when reloading.
		IgnoreInlineComment: true,
	}

	f, err := ini.LoadSources(opt, b)
	if err != nil {
		return nil, err
	}
	return &File{f: f}, nil
}

func NewEmpty() *File {
	opt := ini.LoadOptions{
		SkipUnrecognizableLines: true,
		AllowBooleanKeys:        true,
		IgnoreInlineComment:     true,
	}
	return &File{f: ini.Empty(opt)}
}

func (f *File) Get(section, key string) (string, bool) {
	sec, err := f.f.GetSection(section)
	if err != nil {
		return "", false
	}
	k, err := sec.GetKey(key)
	if err != nil {
		return "", false
	}
	return k.String(), true
}

func (f *File) Set(section, key, value string) {
	if strings.TrimSpace(section) == "" || strings.TrimSpace(key) == "" {
		return
	}
	sec := f.f.Section(section)
	sec.Key(key).SetValue(value)
}

func (f *File) Bytes() []byte {
	// NetworkManager uses GLib keyfile syntax, which is INI-like but does not accept
	// gopkg.in/ini.v1's backtick-quoting for values that contain ';' or '#'.
	// Serialize ourselves to preserve raw values (notably semicolon-delimited lists).
	var buf bytes.Buffer
	for _, sec := range f.f.Sections() {
		name := sec.Name()
		if name == ini.DefaultSection || strings.TrimSpace(name) == "" {
			continue
		}
		_, _ = fmt.Fprintf(&buf, "[%s]\n", name)
		for _, k := range sec.Keys() {
			_, _ = fmt.Fprintf(&buf, "%s=%s\n", k.Name(), k.Value())
		}
		_ = buf.WriteByte('\n')
	}

	b := buf.Bytes()
	if len(b) == 0 {
		return []byte("\n")
	}
	if b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	return b
}
