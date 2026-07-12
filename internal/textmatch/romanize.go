// Package textmatch creates comparison evidence across writing systems. Its
// output is never used as canonical identity or as a replacement display name.
package textmatch

import (
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/gojp/kana"
	ipadict "github.com/ikawaha/kagome-dict/ipa"
	"github.com/ikawaha/kagome/v2/tokenizer"
	"github.com/mozillazg/go-unidecode"
)

var tokenizerOnce sync.Once
var japaneseTokenizer *tokenizer.Tokenizer
var nonAlphanumeric = regexp.MustCompile(`[^\p{L}\p{N}]+`)
var editionAnnotation = regexp.MustCompile(`(?i)\s*[(\[]\s*(deluxe(?:\s+(?:edition|version|package))?|expanded(?:\s+(?:edition|version))?|special(?:\s+edition)?|anniversary(?:\s+edition)?|bonus\s+tracks?(?:\s+(?:edition|version))?|extended(?:\s+(?:edition|version|cut))?|standard(?:\s+(?:edition|version))?|target\s+exclusive|japanese?\s+edition|international(?:\s+(?:edition|version))?|(?:u\.?s\.?|u\.?k\.?)\s+edition|video\s+deluxe)\s*[)\]]`)
var featAnnotation = regexp.MustCompile(`(?i)\s*[(\[]\s*(?:feat\.?|featuring|with)\s+[^)\]]+[)\]]`)

func getTokenizer() *tokenizer.Tokenizer {
	tokenizerOnce.Do(func() {
		value, err := tokenizer.New(ipadict.Dict(), tokenizer.OmitBosEos())
		if err != nil {
			slog.Warn("kagome tokenizer unavailable; Japanese matching is kana-only", "error", err)
			return
		}
		japaneseTokenizer = value
	})
	return japaneseTokenizer
}
func hasJapanese(value string) bool {
	for _, r := range value {
		if unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Han) {
			return true
		}
	}
	return false
}
func allASCII(value string) bool {
	for _, r := range value {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// Romanize returns a comparison form using Kagome compound readings for
// Japanese and Unidecode for Chinese, Thai, Cyrillic, Hangul, and other scripts.
func Romanize(value string) string {
	if allASCII(value) {
		return value
	}
	out := value
	if hasJapanese(value) {
		if t := getTokenizer(); t != nil {
			var reading strings.Builder
			for _, token := range t.Tokenize(value) {
				features := token.Features()
				if len(features) > 7 && features[7] != "" && features[7] != "*" {
					reading.WriteString(features[7])
				} else {
					reading.WriteString(token.Surface)
				}
			}
			out = kana.KanaToRomaji(reading.String())
		} else {
			out = kana.KanaToRomaji(value)
		}
	}
	return unidecode.Unidecode(out)
}
func normalized(value string) string {
	return strings.ToLower(nonAlphanumeric.ReplaceAllString(value, ""))
}
func StripEditionAnnotations(value string) string {
	for {
		next := featAnnotation.ReplaceAllString(editionAnnotation.ReplaceAllString(value, ""), "")
		if next == value {
			break
		}
		value = next
	}
	return strings.Join(strings.Fields(value), " ")
}

// ReleaseKeys returns direct, edition-neutral, and cross-script comparison
// keys. Year zero deliberately permits comparison when a provider omits dates.
func ReleaseKeys(title string, year int) []string {
	keys := []string{}
	add := func(value string) {
		key := normalized(value)
		if key == "" {
			return
		}
		if year > 0 {
			key = fmt.Sprintf("%s|%d", key, year)
		}
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	add(title)
	add(StripEditionAnnotations(title))
	romanized := Romanize(title)
	if romanized != title {
		add(romanized)
		add(StripEditionAnnotations(romanized))
	}
	// Also retain Unidecode's direct script reading. Pure Han text is routed
	// through Kagome above for Japanese compound accuracy; this second key gives
	// Chinese pinyin-style, Thai, Hangul, and other provider romanizations a
	// chance to agree without requiring us to guess the title language.
	universal := unidecode.Unidecode(title)
	if universal != title && universal != romanized {
		add(universal)
		add(StripEditionAnnotations(universal))
	}
	return keys
}
func EquivalentRelease(left string, leftYear int, right string, rightYear int) bool {
	if leftYear > 0 && rightYear > 0 && leftYear != rightYear {
		return false
	}
	for _, a := range ReleaseKeys(left, leftYear) {
		for _, b := range ReleaseKeys(right, rightYear) {
			if a == b {
				return true
			}
			if leftYear == 0 || rightYear == 0 {
				if strings.SplitN(a, "|", 2)[0] == strings.SplitN(b, "|", 2)[0] {
					return true
				}
			}
		}
	}
	return false
}
