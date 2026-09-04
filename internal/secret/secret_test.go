package secret

import (
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

func defaultScanner() *Scanner {
	return New(config.Default("t").Secret.Patterns)
}

func TestCatchesKnownShapes(t *testing.T) {
	cases := map[string]string{
		"api-key":        "키는 sk-abcdefghijklmnopqrstuvwxyz 다",
		"github-token":   "ghp_abcdefghijklmnopqrstuvwxyz01",
		"aws-key":        "AKIAIOSFODNN7EXAMPLE",
		"private-key":    "-----BEGIN RSA PRIVATE KEY-----",
		"password-value": "password = hunter2hunter2",
	}
	scanner := defaultScanner()
	for wanted, line := range cases {
		found := scanner.ScanLine(line)
		if found == nil {
			t.Errorf("%s should have been caught: %s", wanted, line)
			continue
		}
		if found.Rule != wanted {
			t.Errorf("expected rule %s, got %s", wanted, found.Rule)
		}
	}
}

func TestPlainKoreanPasses(t *testing.T) {
	scanner := defaultScanner()
	body := "## 증상\n\n두 글자 한글 질의가 0건이었다.\n\n## 해결\n\n바이그램 구절로 찾는다."
	if found := scanner.ScanText(body); found != nil {
		t.Fatalf("ordinary text must pass, got %+v", found)
	}
}

func TestReportsLineNumberWithoutTheValue(t *testing.T) {
	scanner := defaultScanner()
	body := "첫 줄\n둘째 줄\nghp_abcdefghijklmnopqrstuvwxyz01\n넷째 줄"
	found := scanner.ScanText(body)
	if found == nil || found.Line != 3 {
		t.Fatalf("expected a hit on line 3, got %+v", found)
	}
	if strings.Contains(found.Rule, "ghp_abcdefghijklmnopqrstuvwxyz01") {
		t.Fatal("the finding must not carry the secret")
	}
}

func TestUnnamedPatternReportsItself(t *testing.T) {
	scanner := New([]string{`토큰[0-9]+`})
	found := scanner.ScanLine("토큰1234")
	if found == nil || found.Rule != `토큰[0-9]+` {
		t.Fatalf("a hand written pattern is named by itself, got %+v", found)
	}
}

func TestBrokenPatternIsSkipped(t *testing.T) {
	scanner := New([]string{"([unclosed", `AKIA[0-9A-Z]{16}`})
	if found := scanner.ScanLine("AKIAIOSFODNN7EXAMPLE"); found == nil {
		t.Fatal("a broken pattern must not disable the rest")
	}
}

// TestSecretPatternsCatchKnownShapes walks the table of design 3-4: one line
// per shape, each reported under its own name and not a wider rule's.
func TestSecretPatternsCatchKnownShapes(t *testing.T) {
	cases := map[string]string{
		"anthropic-key":       "sk-ant-api03-abcdefghijklmnopqrstuvwxyz",
		"openai-key":          "sk-proj-abcdefghijklmnopqrstuvwxyz",
		"api-key":             "sk-abcdefghijklmnopqrstuvwxyz",
		"github-token":        "ghp_abcdefghijklmnopqrstuvwxyz01",
		"github-fine-grained": "github_pat_11ABCDE0abcdefghijklmnop",
		"gitlab-token":        "glpat-abcdefghijklmnopqrstuv",
		"slack-token":         "xoxb-1234567890-abcdefghijkl",
		"aws-key":             "AKIAIOSFODNN7EXAMPLE",
		"jwt":                 "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.abcdefghijk",
		"krn-id":              "주민번호 900101-1234567 이다",
		"card-number":         "카드 4111 1111 1111 1111 이다",
		"password-value":      "pw = hunter2",
	}
	scanner := defaultScanner()
	for wanted, line := range cases {
		found := scanner.ScanLine(line)
		if found == nil {
			t.Errorf("%s should have been caught: %s", wanted, line)
			continue
		}
		if found.Rule != wanted {
			t.Errorf("expected rule %s, got %s", wanted, found.Rule)
		}
	}
}

// TestWarnPatternsAreSeparate keeps an address out of the blocking set: lint
// warns about it, add must not refuse it (design 5, question 2).
func TestWarnPatternsAreSeparate(t *testing.T) {
	settings := config.Default("t").Secret
	if found := New(settings.Patterns).ScanLine("보낸 사람 mirusona@example.com"); found != nil {
		t.Fatalf("an address must not block a store, got %+v", found)
	}
	warn := New(settings.WarnPatterns)
	for wanted, line := range map[string]string{
		"email":    "보낸 사람 mirusona@example.com",
		"phone-kr": "전화 010-1234-5678",
	} {
		found := warn.ScanLine(line)
		if found == nil || found.Rule != wanted {
			t.Errorf("expected %s, got %+v", wanted, found)
		}
	}
}

func TestSecretSkipsBrokenPattern(t *testing.T) {
	scanner := New([]string{"[", `AKIA[0-9A-Z]{16}`})
	if len(scanner.Skipped()) != 1 || scanner.Skipped()[0] != "[" {
		t.Fatalf("the broken pattern must be listed, got %v", scanner.Skipped())
	}
	if found := scanner.ScanLine("AKIAIOSFODNN7EXAMPLE"); found == nil {
		t.Fatal("the rest of the rules must stay on")
	}
}

// TestLongBlobLetsPathsThrough is the field test B1 written down: an ordinary
// Unity path, a dotted C# name or a URL must never be read as a key, and a real
// base64 blob must still be.
func TestLongBlobLetsPathsThrough(t *testing.T) {
	scanner := defaultScanner()
	pass := []string{
		"Assets/Scripts/AsmC/Runtime/UI/LoopScrollRect 아래에 있다.",
		"Assets/Scripts/Game/Battle/Controller/BattleFlowController.cs",
		"Assets/Plugins/Android/Firebase/Messaging/Editor/FirebaseMessaging.cs",
		"Assets/Editor/BuildPipeline/Steps/AddressableBuildStep.cs",
		"Assets/Scripts/Framework/Networking/Transport/WebSocketTransport.cs",
		"Assets/Resources/Prefabs/UI/Popup/CommonRewardPopupContainer.prefab",
		"Assets/StreamingAssets/Localization/Tables/KoreanMasterTable.bytes",
		"Assets/Scripts/Gameplay/Match3/Board/BoardMatchResolverService.cs",
		"Assets/ThirdParty/Loxodon/Framework/Runtime/Binding/BindingSet.cs",
		"Packages/com.unity.addressables/Runtime/ResourceManager/AsyncOperations",
		"BattleFlowControllerFactoryProviderRegistry 를 만들었다",
		"Game.Framework.Networking.Transport.WebSocketTransportFactory 를 쓴다",
		"https://docs.unity3d.com/Manual/class-AudioMixerController.html 를 봤다",
		`C:\Users\team\Project\Assets\Scripts\Runtime\UI\LoopScrollRect.cs`,
	}
	for _, line := range pass {
		if found := scanner.ScanLine(line); found != nil {
			t.Errorf("must pass, got rule %s : %s", found.Rule, line)
		}
	}
	block := []string{
		"key = MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvSGmT2xQ",
		"blob aGVsbG93b3JsZGhlbGxvd29ybGRoZWxsb3dvcmxkaGVsbG93b3JsZA==",
		"blob YWJjZGVm/Z2hpamts+bW5vcHFyc3R1dnd4eXowMTIzNDU2Nzg5YWJjZA==",
	}
	for _, line := range block {
		found := scanner.ScanLine(line)
		if found == nil {
			t.Errorf("a base64 blob must be caught: %s", line)
			continue
		}
		if found.Rule != "long-blob" && found.Rule != "password-value" {
			t.Errorf("expected long-blob, got %s : %s", found.Rule, line)
		}
	}
}

// TestPasswordValueNeedsASecretShape covers review #6: a plain memory that
// happens to say "token = 1200" or "secret: patterns" must be storable, while a
// real key on the same shape is still refused.
func TestPasswordValueNeedsASecretShape(t *testing.T) {
	scanner := defaultScanner()
	pass := []string{
		"token = 1200",
		"secret: patterns 를 mem.toml 에서 읽는다",
		"budget token = 1200 으로 잡았다",
		"pw: 비밀번호는 사람이 관리한다",
		"api_key = 없음",
		"tokens = 4000",
	}
	for _, line := range pass {
		if found := scanner.ScanLine(line); found != nil {
			t.Errorf("평범한 기억이 막혔다 (%s) : %s", found.Rule, line)
		}
	}
	block := []string{
		"password = hunter2hunter2",
		"pw = hunter2",
		"api-key: a1b2c3d4e5f6g7h8",
		"token = xkcd_9f2b81ce4400aa31",
		"SECRET=Zm9vYmFyYmF6cXV4",
	}
	for _, line := range block {
		found := scanner.ScanLine(line)
		if found == nil {
			t.Errorf("진짜 키가 안 막혔다 : %s", line)
			continue
		}
		if found.Rule != "password-value" && found.Rule != "long-blob" {
			t.Errorf("password-value 로 잡혀야 하는데 %s 다 : %s", found.Rule, line)
		}
	}
}

// TestPasswordWordOnlyWarns keeps the loose shape in the warning set, so lint
// still mentions the line that mem add now stores.
func TestPasswordWordOnlyWarns(t *testing.T) {
	warn := New(config.Default("t").Secret.WarnPatterns)
	found := warn.ScanLine("secret: patterns")
	if found == nil || found.Rule != "password-word" {
		t.Fatalf("lint 이 짚어 줄 경고 규칙이 없다 : %+v", found)
	}
}
