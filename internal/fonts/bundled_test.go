package fonts

import (
	"os"
	"testing"
)

func TestBundledFontsIndexed(t *testing.T) {
	idx := NewIndex()
	list := idx.List()

	bundledFamilies := map[string]int{}
	for _, f := range list {
		if f.Source == SourceBundled {
			bundledFamilies[f.Family]++
			if f.ID == "" {
				t.Fatalf("bundled font %q has no id", f.Family)
			}
			if f.Path == "" {
				t.Fatalf("bundled font %q was not extracted", f.Family)
			}
			if _, err := os.Stat(f.Path); err != nil {
				t.Fatalf("extracted font missing: %v", err)
			}
		}
	}

	for _, family := range []string{
		"Inter", "Poppins", "Montserrat", "Nunito", "Plus Jakarta Sans",
		"DM Sans", "Manrope", "Roboto", "Bebas Neue", "Anton", "Oswald",
		"Archivo Black", "Space Grotesk", "Playfair Display", "Lora",
		"Merriweather", "JetBrains Mono",
	} {
		if bundledFamilies[family] == 0 {
			t.Errorf("bundled family %q not indexed", family)
		}
	}

	data, err := BundledFile("inter-700")
	if err != nil || len(data) < 1000 {
		t.Fatalf("BundledFile(inter-700) = %d bytes, err=%v", len(data), err)
	}
	if _, err := BundledFile("../evil"); err == nil {
		t.Fatal("BundledFile must reject path traversal")
	}

	face := idx.ResolveFace("Inter", true, false)
	if face == "" {
		t.Fatal("ResolveFace(Inter, bold) returned empty")
	}
	if _, err := os.Stat(face); err != nil {
		t.Fatalf("resolved face missing: %v", err)
	}
}
