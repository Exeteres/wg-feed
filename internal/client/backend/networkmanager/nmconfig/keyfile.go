package nmconfig

import (
	"bytes"
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
	}
	return &File{f: ini.Empty(opt)}
}

func (f *File) HasSection(section string) bool {
	_, err := f.f.GetSection(section)
	return err == nil
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

func (f *File) RemoveSectionsWithPrefix(prefix string) {
	if prefix == "" {
		return
	}
	for _, sec := range f.f.Sections() {
		name := sec.Name()
		if strings.HasPrefix(name, prefix) {
			f.f.DeleteSection(name)
		}
	}
}

func (f *File) Bytes() []byte {
	var buf bytes.Buffer
	_, _ = f.f.WriteTo(&buf)
	b := buf.Bytes()
	if len(b) == 0 || b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	return b
}
