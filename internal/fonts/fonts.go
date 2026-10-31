// Package fonts indexes installed system fonts so the editor can offer a
// font picker and the renderer can resolve a font file for drawtext.
package fonts

import (
	"bytes"
	"embed"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
)

const (
	// SourceBundled marks fonts shipped inside the app (Google Fonts, OFL).
	SourceBundled = "bundled"
	// SourceSystem marks fonts installed on the machine.
	SourceSystem = "system"
)

//go:embed bundled/*.ttf
var bundledTTFs embed.FS

// Font is one installed font face.
type Font struct {
	ID        string `json:"id"`
	Family    string `json:"family"`
	Subfamily string `json:"subfamily"`
	FullName  string `json:"full_name"`
	Path      string `json:"path"`
	Source    string `json:"source"`
}

// Index is a lazily loaded catalog of installed fonts.
type Index struct {
	mu       sync.Mutex
	loaded   bool
	fonts    []Font
	byFamily map[string]Font
}

func NewIndex() *Index {
	return &Index{}
}

// BundledDir is where embedded fonts are extracted so ffmpeg/libass can read
// them as regular files. It lives in the per-user cache directory.
func BundledDir() string {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "AutoCut", "fonts")
	}
	return filepath.Join(os.TempDir(), "autocut-fonts")
}

// BundledFile returns the embedded font bytes for an ID such as "inter-700".
func BundledFile(id string) ([]byte, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return nil, os.ErrNotExist
	}
	for _, r := range id {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if !valid {
			return nil, os.ErrNotExist
		}
	}
	return bundledTTFs.ReadFile("bundled/" + id + ".ttf")
}

// ExportBundled writes every embedded font into dir (idempotent). The renderer
// uses it to build a fonts directory for libass without touching system fonts.
func ExportBundled(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	entries, err := bundledTTFs.ReadDir("bundled")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".ttf") {
			continue
		}
		data, err := bundledTTFs.ReadFile("bundled/" + entry.Name())
		if err != nil {
			continue
		}
		target := filepath.Join(dir, entry.Name())
		if info, err := os.Stat(target); err == nil && info.Size() == int64(len(data)) {
			continue
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			return err
		}
	}
	return nil
}

// ExportFace copies the resolved face for a family into dir so libass can find
// it through fontsdir. It is a no-op when the face cannot be resolved.
func (i *Index) ExportFace(dir, family string, bold, italic bool) error {
	face := i.ResolveFace(family, bold, italic)
	if face == "" {
		return nil
	}
	info, err := os.Stat(face)
	if err != nil || info.IsDir() {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	target := filepath.Join(dir, filepath.Base(face))
	if existing, err := os.Stat(target); err == nil && existing.Size() == info.Size() {
		return nil
	}
	data, err := os.ReadFile(face)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0644)
}

func fontDirs() []string {
	var dirs []string
	if win := os.Getenv("WINDIR"); win != "" {
		dirs = append(dirs, filepath.Join(win, "Fonts"))
	} else {
		dirs = append(dirs, `C:\Windows\Fonts`)
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		dirs = append(dirs, filepath.Join(local, "Microsoft", "Windows", "Fonts"))
	}
	return dirs
}

// List returns every indexed font face sorted by family name.
func (i *Index) List() []Font {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.loadLocked()
	out := make([]Font, len(i.fonts))
	copy(out, i.fonts)
	return out
}

// Find resolves a font family to a face, preferring a Regular weight.
func (i *Index) Find(family string) (Font, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.loadLocked()

	key := strings.ToLower(strings.TrimSpace(family))
	if f, ok := i.byFamily[key]; ok {
		return f, true
	}
	return Font{}, false
}

// ResolveFace picks the closest installed face for a family with the requested weight/style.
func (i *Index) ResolveFace(family string, bold, italic bool) string {
	i.mu.Lock()
	i.loadLocked()

	key := strings.ToLower(strings.TrimSpace(family))
	var best *Font
	bestScore := -1
	for idx := range i.fonts {
		f := &i.fonts[idx]
		if strings.ToLower(f.Family) != key {
			continue
		}
		sub := strings.ToLower(f.Subfamily)
		score := 0
		hasBold := strings.Contains(sub, "bold") || strings.Contains(sub, "black") || strings.Contains(sub, "semibold")
		hasItalic := strings.Contains(sub, "italic") || strings.Contains(sub, "oblique")
		if hasBold == bold {
			score += 2
		}
		if hasItalic == italic {
			score += 1
		}
		if score > bestScore {
			bestScore = score
			best = f
		}
	}
	if best != nil {
		path := best.Path
		i.mu.Unlock()
		return path
	}
	i.mu.Unlock()
	return i.Resolve(family, "")
}

// Resolve returns a font file for a family, falling back to Arial.
func (i *Index) Resolve(family, explicitPath string) string {
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err == nil {
			return explicitPath
		}
	}
	if f, ok := i.Find(family); ok && f.Path != "" {
		return f.Path
	}
	fallbacks := []string{
		`C:\Windows\Fonts\arial.ttf`,
		`C:\Windows\Fonts\segoeui.ttf`,
	}
	for _, p := range fallbacks {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func (i *Index) loadLocked() {
	if i.loaded {
		return
	}
	i.loaded = true
	i.byFamily = map[string]Font{}

	seen := map[string]bool{}
	i.loadBundledLocked(seen)
	for _, dir := range fontDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if ext != ".ttf" && ext != ".otf" && ext != ".ttc" {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			family, subfamily, fullName := readFontNames(path)
			if family == "" {
				continue
			}
			i.addFontLocked(Font{
				Family:    family,
				Subfamily: subfamily,
				FullName:  fullName,
				Path:      path,
				Source:    SourceSystem,
			}, seen)
		}
	}

	sort.Slice(i.fonts, func(a, b int) bool {
		if i.fonts[a].Family == i.fonts[b].Family {
			return i.fonts[a].Subfamily < i.fonts[b].Subfamily
		}
		return i.fonts[a].Family < i.fonts[b].Family
	})
}

func (i *Index) loadBundledLocked(seen map[string]bool) {
	dir := BundledDir()
	entries, err := bundledTTFs.ReadDir("bundled")
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".ttf") {
			continue
		}
		data, err := bundledTTFs.ReadFile("bundled/" + entry.Name())
		if err != nil {
			continue
		}
		family, subfamily, fullName := readFontNamesAt(bytes.NewReader(data))
		if family == "" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		path := filepath.Join(dir, entry.Name())
		if info, err := os.Stat(path); err != nil || info.Size() != int64(len(data)) {
			if err := os.MkdirAll(dir, 0755); err == nil {
				if err := os.WriteFile(path, data, 0644); err != nil {
					path = ""
				}
			} else {
				path = ""
			}
		}
		i.addFontLocked(Font{
			ID:        id,
			Family:    family,
			Subfamily: subfamily,
			FullName:  fullName,
			Path:      path,
			Source:    SourceBundled,
		}, seen)
	}
}

func (i *Index) addFontLocked(font Font, seen map[string]bool) {
	dedupeKey := strings.ToLower(font.Family + "|" + font.Subfamily)
	if seen[dedupeKey] {
		return
	}
	seen[dedupeKey] = true
	i.fonts = append(i.fonts, font)

	familyKey := strings.ToLower(font.Family)
	existing, ok := i.byFamily[familyKey]
	if !ok || isRegular(font.Subfamily) && !isRegular(existing.Subfamily) {
		i.byFamily[familyKey] = font
	}
}

func isRegular(subfamily string) bool {
	s := strings.ToLower(subfamily)
	return !strings.Contains(s, "italic") && !strings.Contains(s, "bold") &&
		!strings.Contains(s, "light") && !strings.Contains(s, "black") &&
		!strings.Contains(s, "condensed")
}

// readFontNames extracts family/subfamily/full name from a TTF, OTF or TTC file.
func readFontNames(path string) (family, subfamily, fullName string) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", ""
	}
	defer file.Close()
	return readFontNamesAt(file)
}

func readFontNamesAt(file io.ReaderAt) (family, subfamily, fullName string) {
	header := make([]byte, 12)
	if _, err := file.ReadAt(header, 0); err != nil {
		return "", "", ""
	}

	offset := int64(0)
	if string(header[0:4]) == "ttcf" {
		first := make([]byte, 4)
		if _, err := file.ReadAt(first, 12); err != nil {
			return "", "", ""
		}
		offset = int64(binary.BigEndian.Uint32(first))
		if _, err := file.ReadAt(header, offset); err != nil {
			return "", "", ""
		}
	}

	numTables := int(binary.BigEndian.Uint16(header[4:6]))
	if numTables <= 0 || numTables > 512 {
		return "", "", ""
	}

	records := make([]byte, numTables*16)
	if _, err := file.ReadAt(records, offset+12); err != nil {
		return "", "", ""
	}

	var nameOffset, nameLength int64
	for i := 0; i < numTables; i++ {
		rec := records[i*16 : i*16+16]
		if string(rec[0:4]) == "name" {
			nameOffset = int64(binary.BigEndian.Uint32(rec[8:12]))
			nameLength = int64(binary.BigEndian.Uint32(rec[12:16]))
			break
		}
	}
	if nameOffset == 0 || nameLength <= 0 || nameLength > 4<<20 {
		return "", "", ""
	}

	nameTable := make([]byte, nameLength)
	if _, err := file.ReadAt(nameTable, nameOffset); err != nil {
		return "", "", ""
	}
	return parseNameTable(nameTable)
}

func parseNameTable(table []byte) (family, subfamily, fullName string) {
	if len(table) < 6 {
		return "", "", ""
	}
	count := int(binary.BigEndian.Uint16(table[2:4]))
	stringOffset := int(binary.BigEndian.Uint16(table[4:6]))
	if count <= 0 || count > 4096 || 6+count*12 > len(table) {
		return "", "", ""
	}

	best := map[uint16]int{} // nameID -> record index
	bestScore := map[uint16]int{}
	for i := 0; i < count; i++ {
		rec := table[6+i*12 : 6+i*12+12]
		platformID := binary.BigEndian.Uint16(rec[0:2])
		encodingID := binary.BigEndian.Uint16(rec[2:4])
		languageID := binary.BigEndian.Uint16(rec[4:6])
		nameID := binary.BigEndian.Uint16(rec[6:8])

		score := 0
		if platformID == 3 && encodingID == 1 {
			score += 4
		}
		if platformID == 0 {
			score += 3
		}
		if platformID == 1 && encodingID == 0 {
			score += 2
		}
		if languageID == 0x0409 {
			score += 2
		}
		if cur, ok := bestScore[nameID]; !ok || score > cur {
			bestScore[nameID] = score
			best[nameID] = i
		}
	}

	readName := func(nameID uint16) string {
		idx, ok := best[nameID]
		if !ok {
			return ""
		}
		rec := table[6+idx*12 : 6+idx*12+12]
		platformID := binary.BigEndian.Uint16(rec[0:2])
		length := int(binary.BigEndian.Uint16(rec[8:10]))
		offset := stringOffset + int(binary.BigEndian.Uint16(rec[10:12]))
		if offset < 0 || length <= 0 || offset+length > len(table) {
			return ""
		}
		raw := table[offset : offset+length]
		if platformID == 3 || platformID == 0 {
			return decodeUTF16BE(raw)
		}
		return string(raw)
	}

	family = strings.TrimSpace(readName(1))
	subfamily = strings.TrimSpace(readName(2))
	fullName = strings.TrimSpace(readName(4))
	if family == "" {
		family = strings.TrimSpace(readName(16)) // typographic family
	}
	return family, subfamily, fullName
}

func decodeUTF16BE(raw []byte) string {
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		units = append(units, binary.BigEndian.Uint16(raw[i:i+2]))
	}
	return string(utf16.Decode(units))
}
