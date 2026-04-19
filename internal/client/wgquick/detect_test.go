package wgquick

import "testing"

func TestHasAmneziaExtensions(t *testing.T) {
	tests := []struct {
		name string
		cfg  string
		want bool
	}{
		{
			name: "plain wg",
			cfg:  "[Interface]\nPrivateKey=x\n[Peer]\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
			want: false,
		},
		{
			name: "amnezia jc",
			cfg:  "[Interface]\nPrivateKey=x\nJc = 3\n[Peer]\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
			want: true,
		},
		{
			name: "amnezia i-tag",
			cfg:  "[Interface]\nPrivateKey=x\nI2=<r 16><t>\n[Peer]\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
			want: true,
		},
		{
			name: "comment ignored",
			cfg:  "[Interface]\nPrivateKey=x\n# jc = 3\n; s1 = 10\n[Peer]\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := HasAmneziaExtensions(tc.cfg)
			if got != tc.want {
				t.Fatalf("HasAmneziaExtensions()=%v want=%v", got, tc.want)
			}
		})
	}
}
