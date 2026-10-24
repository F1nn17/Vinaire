package backup

import "testing"

// Found by FuzzLoadConfig: the bucket pattern let ".." through ("0000000..0").
func TestAdjacentPeriodsAreNeverAcceptedInABucketOrAPrefix(t *testing.T) {
	ok := S3Target{Name: "s3", Endpoint: "https://s3.example.com", Bucket: "backups", Prefix: "vinaire/db"}
	if err := (&Config{Targets: []S3Target{ok}}).Validate(); err != nil {
		t.Fatalf("a good target was refused: %v", err)
	}
	for name, mod := range map[string]func(*S3Target){
		"bucket":       func(x *S3Target) { x.Bucket = "0000000..0" },
		"bucket dots":  func(x *S3Target) { x.Bucket = "my..bucket" },
		"prefix":       func(x *S3Target) { x.Prefix = "a..b" },
		"prefix climb": func(x *S3Target) { x.Prefix = "a/../b" },
	} {
		x := ok
		mod(&x)
		if err := (&Config{Targets: []S3Target{x}}).Validate(); err == nil {
			t.Errorf("%s: %+v was accepted", name, x)
		}
	}
}
