package publicaddress

import "testing"

func TestPublicIPClassification(t *testing.T) {
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2606:4700::1111", "::ffff:8.8.8.8"} {
		if _, e := Parse(ip); e != nil {
			t.Fatalf("%s：%v", ip, e)
		}
	}
	for _, ip := range []string{"10.1.2.3", "100.100.201.91", "192.168.1.2", "127.0.0.1", "169.254.1.1", "192.0.2.1", "198.18.0.1", "255.255.255.255", "fd00::1", "fe80::1", "::1", "2001:db8::1", "8.8.8.8/24"} {
		if _, e := Parse(ip); e == nil {
			t.Fatalf("误接受 %s", ip)
		}
	}
}
