package replace

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/Jszigeti/floe/internal/config"
	"github.com/Jszigeti/floe/internal/run"
)

// Domain is a preferences domain; CurrentHost reads and writes its ByHost file.
type Domain struct {
	Name        string
	CurrentHost bool
}

var appleDomains = []Domain{
	{Name: "com.apple.dock"},
	{Name: "com.apple.finder"},
	{Name: "NSGlobalDomain"},
	{Name: "com.apple.HIToolbox"},
	{Name: "com.apple.symbolichotkeys"},
	{Name: "com.apple.AppleMultitouchTrackpad"},
	{Name: "com.apple.driver.AppleBluetoothMultitouch.trackpad"},
	{Name: "com.apple.AppleMultitouchMouse"},
	{Name: "com.apple.driver.AppleBluetoothMultitouch.mouse"},
	{Name: "NSGlobalDomain", CurrentHost: true},
	{Name: "com.apple.controlcenter"},
	{Name: "com.apple.menuextra.clock"},
	{Name: "com.apple.screencapture"},
	{Name: "com.apple.WindowManager"},
	{Name: "com.apple.spaces"},
	{Name: "com.apple.universalaccess"},
}

var traceWords = []string{"Recent", "Window", "Frame", "Date", "Time", "Count", "Last", "UUID", "Identifier", "Token", "History", "Bookmark"}

// IsTrace reports whether a key records what happened on the old Mac rather than a choice the user made.
func IsTrace(key string, value []byte, oldUUID string) bool {
	if strings.HasPrefix(key, "AK") || slices.ContainsFunc(traceWords, func(w string) bool { return strings.Contains(key, w) }) {
		return true
	}
	if oldUUID != "" && bytes.Contains(bytes.ToUpper(value), []byte(strings.ToUpper(oldUUID))) {
		return true
	}
	return dataSize(value) > 2048
}

// CarrySettings writes every Apple setting of the staged plists that differs from this Mac's and is not a trace.
func CarrySettings(ctx context.Context, r run.Runner, plists map[Domain]string, oldHome, newHome, oldUUID string) (carried int, errs []error) {
	rehome := config.Rehomer(oldHome, newHome)
	for _, d := range appleDomains {
		if path, ok := plists[d]; ok {
			n, domainErrs := carryDomain(ctx, r, d, path, rehome, oldUUID)
			carried += n
			errs = append(errs, domainErrs...)
		}
	}
	if carried > 0 {
		if _, err := r.Run(ctx, run.Cmd{Name: "killall", Args: []string{"Dock", "Finder", "SystemUIServer", "ControlCenter"}}); err != nil {
			errs = append(errs, err)
		}
	}
	return carried, errs
}

func carryDomain(ctx context.Context, r run.Runner, d Domain, path string, rehome func(string) string, oldUUID string) (int, []error) {
	old, err := readValues(ctx, r, run.Cmd{Name: "plutil", Args: []string{"-convert", "xml1", "-o", "-", path}})
	if err != nil {
		return 0, []error{fmt.Errorf("%s: %w", d.Name, err)}
	}
	current, err := readValues(ctx, r, run.Cmd{Name: "defaults", Args: defaultsArgs(d, "export", d.Name, "-")})
	if err != nil {
		return 0, []error{fmt.Errorf("%s: %w", d.Name, err)}
	}
	carried := 0
	var errs []error
	for _, key := range slices.Sorted(maps.Keys(old)) {
		value := rehome(old[key])
		if canon(value) == canon(current[key]) || IsTrace(key, []byte(value), oldUUID) {
			continue
		}
		if err := writeKey(ctx, r, d, path, key, rehome); err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", d.Name, key, err))
			continue
		}
		carried++
	}
	return carried, errs
}

func writeKey(ctx context.Context, r run.Runner, d Domain, path, key string, rehome func(string) string) error {
	// plutil reads a dot as a key path separator, and the global domain is full of dotted keys.
	value, err := r.Run(ctx, run.Cmd{Name: "plutil", Args: []string{"-extract", strings.ReplaceAll(key, ".", `\.`), "xml1", "-o", "-", path}})
	if err != nil {
		return err
	}
	_, err = r.Run(ctx, run.Cmd{Name: "defaults", Args: defaultsArgs(d, "write", d.Name, key, rehome(value))})
	return err
}

func defaultsArgs(d Domain, args ...string) []string {
	if d.CurrentHost {
		return append([]string{"-currentHost"}, args...)
	}
	return args
}

func readValues(ctx context.Context, r run.Runner, c run.Cmd) (map[string]string, error) {
	out, err := r.Run(ctx, c)
	if err != nil {
		return nil, err
	}
	return topLevel(out)
}

// topLevel maps each key of a plist's root dictionary to the XML of its value.
func topLevel(doc string) (map[string]string, error) {
	d := xml.NewDecoder(strings.NewReader(doc))
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("no root dictionary: %w", err)
		}
		if start, ok := tok.(xml.StartElement); ok && start.Name.Local == "dict" {
			break
		}
	}
	values := map[string]string{}
	key := ""
	for {
		offset := d.InputOffset()
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return values, nil
		case xml.StartElement:
			if t.Name.Local == "key" {
				if err := d.DecodeElement(&key, &t); err != nil {
					return nil, err
				}
			} else {
				if err := d.Skip(); err != nil {
					return nil, err
				}
				values[key] = doc[offset:d.InputOffset()]
			}
		}
	}
}

// canon reduces a plist value to its content, so indentation never makes two equal values differ.
func canon(raw string) string {
	d := xml.NewDecoder(strings.NewReader(raw))
	var b strings.Builder
	open := ""
	for {
		tok, err := d.Token()
		if err != nil {
			return b.String()
		}
		switch t := tok.(type) {
		case xml.StartElement:
			open = t.Name.Local
			b.WriteString("<" + open + ">")
		case xml.EndElement:
			open = ""
			b.WriteString("</" + t.Name.Local + ">")
		case xml.CharData:
			switch open {
			case "", "dict", "array":
			case "data":
				b.WriteString(strings.Join(strings.Fields(string(t)), ""))
			default:
				b.WriteString(strconv.Quote(string(t)))
			}
		}
	}
}

// dataSize is the decoded size of a value whose root is a data element, else 0.
func dataSize(value []byte) int {
	d := xml.NewDecoder(bytes.NewReader(value))
	for {
		tok, err := d.Token()
		if err != nil {
			return 0
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local == "plist" {
			continue
		}
		var encoded string
		if start.Name.Local != "data" || d.DecodeElement(&encoded, &start) != nil {
			return 0
		}
		return base64.StdEncoding.DecodedLen(len(strings.Join(strings.Fields(encoded), "")))
	}
}
