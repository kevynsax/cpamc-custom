package agyproxy

// CUSTOM (cpamc-custom)

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/json"
	"github.com/MoeForge/moego/ojson"
	"github.com/MoeForge/moego/regexp"
)

var credentialText = regexp.MustCompile(`(?i)((?:access[_-]?token|refresh[_-]?token|id[_-]?token|client[_-]?secret|authorization|cookie|api[_-]?key)["']?\s*[=:]\s*)[^&\r\n,;]+`)
var bearerText = regexp.MustCompile(`(?i)\bBearer\s+[a-z0-9._~+/=-]+`)

type jsonLogField struct {
	name  string
	value ojson.Result
}

func sensitive(name string) bool {
	lower := strings.ToLower(name)
	lower = strings.ReplaceAll(lower, "-", "_")
	credentialToken := strings.HasSuffix(lower, "token") || lower == "tokens" || strings.Contains(lower, "access_token") || strings.Contains(lower, "refresh_token") || strings.Contains(lower, "id_token")
	return strings.Contains(lower, "authorization") || strings.Contains(lower, "cookie") || credentialToken || strings.Contains(lower, "secret") || strings.Contains(lower, "api_key") || strings.Contains(lower, "apikey") || lower == "key"
}

func maskedHeaders(headers http.Header) http.Header {
	masked := headers.Clone()
	for name, values := range masked {
		if sensitive(name) {
			masked[name] = []string{"[MASKED]"}
			continue
		}
		for i, value := range values {
			masked[name][i] = maskText(value)
		}
	}
	return masked
}

func maskText(text string) string {
	masked := credentialText.ReplaceAllString(text, "${1}[MASKED]")
	masked = bearerText.ReplaceAllString(masked, "Bearer [MASKED]")
	return masked
}

func maskBody(data []byte) []byte {
	if json.Valid(data) {
		value := ojson.ParseBytes(data)
		doc := ojson.NewDoc(data)
		if !maskJSON(doc, value, "") {
			return []byte("[content redaction failed]")
		}
		return doc.Bytes()
	}
	text := string(data)
	if form, errParse := url.ParseQuery(text); errParse == nil && strings.Contains(text, "=") && !strings.ContainsAny(text, "\r\n") {
		for name, values := range form {
			if sensitive(name) {
				form[name] = []string{"[MASKED]"}
				continue
			}
			for i, value := range values {
				form[name][i] = maskText(value)
			}
		}
		encoded := form.Encode()
		return []byte(encoded)
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		prefix := ""
		candidate := line
		if strings.HasPrefix(line, "data:") {
			prefix = "data:"
			candidate = strings.TrimPrefix(line, prefix)
		}
		candidateBytes := []byte(candidate)
		if json.Valid(candidateBytes) {
			decoded := ojson.ParseBytes(candidateBytes)
			doc := ojson.NewDoc(candidateBytes)
			if !maskJSON(doc, decoded, "") {
				lines[i] = prefix + "[content redaction failed]"
				continue
			}
			lines[i] = prefix + doc.String()
			continue
		}
		lines[i] = maskText(line)
	}
	result := strings.Join(lines, "\n")
	return []byte(result)
}

func maskJSON(doc *ojson.Doc, value ojson.Result, path string) bool {
	if value.Type == ojson.String {
		text := value.String()
		masked := maskText(text)
		if masked == text {
			return true
		}
		errSet := doc.Set(path, masked)
		return errSet == nil
	}
	if !value.IsObject() && !value.IsArray() {
		return true
	}
	var fields []jsonLogField
	counts := make(map[string]int)
	isArray := value.IsArray()
	collect := func(key, child ojson.Result) bool {
		name := key.String()
		if isArray {
			index := len(fields)
			name = strconv.Itoa(index)
		}
		field := jsonLogField{name: name, value: child}
		fields = append(fields, field)
		counts[name]++
		return true
	}
	value.ForEach(collect)
	replacer := strings.NewReplacer("\\", "\\\\", ".", "\\.", ":", "\\:", "*", "\\*", "?", "\\?")
	for _, field := range fields {
		escaped := replacer.Replace(field.name)
		childPath := escaped
		if path != "" {
			childPath = path + "." + escaped
		}
		duplicate := counts[field.name] > 1
		if duplicate || (!isArray && sensitive(field.name)) {
			if duplicate {
				for {
					current := doc.Get(childPath)
					if !current.Exists() {
						break
					}
					if errDelete := doc.Delete(childPath); errDelete != nil {
						return false
					}
				}
			}
			if errSet := doc.Set(childPath, "[MASKED]"); errSet != nil {
				return false
			}
			continue
		}
		if !maskJSON(doc, field.value, childPath) {
			return false
		}
	}
	return true
}
