package install

// mem install 의 임베딩 단계 (결정 17·18·19 + 사용자 결정 2026-08-23).
//
// **임베딩은 선택이 아니라 기본 설치다.** `--no-embed` 를 줘야 뺀다.
// 못 갖추면(오프라인 · 해시 불일치) **경고를 남기고 낱말 모드로 설치를 끝낸다.**
// 설치가 죽으면 안 된다 — 의미 검색은 얹는 것이지 있어야 도는 것이 아니다.
//
// 받는 자리는 셋 다 `~/.aimemory/` 안이다. **임시 폴더에 DLL 을 풀지 않는다** —
// 그건 악성코드가 하는 짓이라 Defender/EDR 이 잡는다 (결정 19).

import (
	"archive/zip"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/embed"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// fetchTimeout 은 내려받기 한 번의 상한이다. 없으면 오프라인에서 영원히 멈춘다.
const fetchTimeout = 10 * time.Minute

// embedStep 은 임베딩 파일 상태를 한 줄로 만들고, apply 면 실제로 놓는다.
// 안내 글은 report 에 붙인다.
func embedStep(report *Report, options Options, dryRun bool) {
	model := options.Model
	if model == "" {
		model = embed.DefaultModel
	}
	runtimeAsset := embed.RuntimeAsset()
	wants := append([]embed.Asset{runtimeAsset}, embed.ModelAssets(model)...)
	targets := map[string]string{runtimeAsset.Name: embed.DLLPath()}
	for _, asset := range embed.ModelAssets(model) {
		targets[asset.Name] = filepath.Join(embed.ModelDir(model), asset.Name)
	}
	runtimeStep := Step{What: i18n.T(i18n.EmbedStepRuntime)}
	modelStep := Step{What: i18n.T(i18n.EmbedStepModel) + " (" + model + ")"}
	if options.NoEmbed {
		runtimeStep.Now, runtimeStep.Todo = stateOf(targets[runtimeAsset.Name], runtimeAsset), i18n.T(i18n.EmbedTodoSkip)
		modelStep.Now, modelStep.Todo = modelState(model, targets), i18n.T(i18n.EmbedTodoSkip)
		report.add(runtimeStep)
		report.add(modelStep)
		report.note(i18n.T(i18n.EmbedOff))
		return
	}
	runtimeStep.Now = stateOf(targets[runtimeAsset.Name], runtimeAsset)
	modelStep.Now = modelState(model, targets)
	runtimeStep.Todo = todoOf(runtimeStep.Now, options)
	modelStep.Todo = todoOf(modelStep.Now, options)
	runtimeStep.Changed = runtimeStep.Todo != i18n.T(i18n.EmbedTodoKeep)
	modelStep.Changed = modelStep.Todo != i18n.T(i18n.EmbedTodoKeep)
	if dryRun {
		report.add(runtimeStep)
		report.add(modelStep)
		if runtimeStep.Changed || modelStep.Changed {
			report.note(i18n.T(i18n.EmbedSizeNote))
		}
		return
	}
	failed := []string{}
	runtimeFailed, modelFailed := false, false
	for _, asset := range wants {
		target := targets[asset.Name]
		if embed.SHAOK(target, asset.SHA) {
			continue
		}
		if err := place(asset, target, options); err != nil {
			failed = append(failed, asset.Name+" : "+err.Error())
			if asset.Name == runtimeAsset.Name {
				runtimeFailed = true
			} else {
				modelFailed = true
			}
		}
	}
	// 못 놓은 소단계는 「고쳤다」로 세면 안 된다. 표의 할 일 칸은 「꾸러미에서
	// 복사」인데 꼬리말이 「4 가지를 고쳤다」라고 말하면 거짓 보고다
	// (실데이터 시험 C7).
	markFailed(&runtimeStep, runtimeFailed)
	markFailed(&modelStep, modelFailed)
	report.add(runtimeStep)
	report.add(modelStep)
	if len(failed) > 0 {
		// 한 파일이라도 못 갖추면 **모델 폴더를 통째로 지운다** (W4). 예전에는
		// 깨진 파일 하나만 지워서 모델·토크나이저 129MiB 가 죽은 무게로 홈에
		// 남았고 아무도 안 알려 줬다. 어차피 한 짝이 없으면 안 돈다.
		if modelFailed {
			if err := os.RemoveAll(embed.ModelDir(model)); err == nil {
				report.note(i18n.T(i18n.EmbedModelWiped, embed.ModelDir(model)))
			}
		}
		report.note(i18n.T(i18n.EmbedFailed, joinLines(failed)))
		if options.Bundle == "" {
			report.note(i18n.T(i18n.EmbedNoSource))
		}
		return
	}
	placeLicenses(model, options)
	report.note(i18n.T(i18n.EmbedDone, model))
}

// markFailed 는 못 놓은 소단계의 「할 일」 칸을 실패로 바꾸고 셈에서 뺀다.
func markFailed(step *Step, failed bool) {
	if !failed {
		return
	}
	step.Todo, step.Changed = i18n.T(i18n.EmbedTodoFailed), false
}

// placeLicenses 는 라이선스 전문을 파일 옆에 같이 놓는다. 없으면 그냥 넘어간다 —
// 라이선스가 없다고 설치를 막지는 않되, 있으면 반드시 같이 놓는다
// (THIRD-PARTY.md 「같이 놓는 파일」).
func placeLicenses(model string, options Options) {
	if options.Bundle == "" {
		return
	}
	pairs := [][2]string{
		{filepath.Join("bin", "LICENSE-onnxruntime.txt"), filepath.Join(filepath.Dir(embed.DLLPath()), "LICENSE-onnxruntime.txt")},
		{filepath.Join("models", model, "LICENSE.txt"), filepath.Join(embed.ModelDir(model), "LICENSE.txt")},
	}
	for _, pair := range pairs {
		source := filepath.Join(options.Bundle, pair[0])
		if _, err := os.Stat(source); err != nil {
			continue
		}
		copyStream(source, pair[1])
	}
}

func joinLines(items []string) string {
	out := ""
	for at, item := range items {
		if at > 0 {
			out += "\n  "
		}
		out += item
	}
	return out
}

// stateOf 는 파일 하나의 「지금」 칸이다.
func stateOf(path string, asset embed.Asset) string {
	if _, err := os.Stat(path); err != nil {
		return i18n.T(i18n.EmbedStateNone)
	}
	if !embed.SizeOK(path, asset.Bytes) {
		return i18n.T(i18n.EmbedStateBroken)
	}
	return i18n.T(i18n.EmbedStateOK)
}

// modelState 는 모델 파일 셋을 한 칸으로 묶는다. 하나라도 모자라면 없음이다.
func modelState(model string, targets map[string]string) string {
	worst := i18n.T(i18n.EmbedStateOK)
	for _, asset := range embed.ModelAssets(model) {
		switch stateOf(targets[asset.Name], asset) {
		case i18n.T(i18n.EmbedStateNone):
			return i18n.T(i18n.EmbedStateNone)
		case i18n.T(i18n.EmbedStateBroken):
			worst = i18n.T(i18n.EmbedStateBroken)
		}
	}
	return worst
}

func todoOf(now string, options Options) string {
	if now == i18n.T(i18n.EmbedStateOK) {
		return i18n.T(i18n.EmbedTodoKeep)
	}
	if options.Bundle != "" {
		return i18n.T(i18n.EmbedTodoBundle)
	}
	return i18n.T(i18n.EmbedTodoFetch)
}

// place 는 파일 하나를 자리에 놓는다. **놓은 뒤 해시를 다시 재고, 안 맞으면
// 지운다** — 검증 안 한 바이너리를 남겨 두지 않는다 (결정 18).
func place(asset embed.Asset, target string, options Options) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	var err error
	if options.Bundle != "" {
		err = fromBundle(asset, target, options.Bundle)
	} else {
		err = fromNet(asset, target)
	}
	if err != nil {
		os.Remove(target)
		return err
	}
	if !embed.SHAOK(target, asset.SHA) {
		os.Remove(target)
		return errors.New(i18n.T(i18n.EmbedSHABad, asset.Name))
	}
	return nil
}

// fromBundle 은 오프라인 꾸러미 폴더에서 복사한다. 자리는 asset.Rel 이다.
func fromBundle(asset embed.Asset, target, bundle string) error {
	source := filepath.Join(bundle, filepath.FromSlash(asset.Rel))
	if _, err := os.Stat(source); err != nil {
		// 꾸러미를 평평하게 놓은 사람도 있다. 파일 이름으로 한 번 더 본다.
		flat := filepath.Join(bundle, asset.Name)
		if _, second := os.Stat(flat); second != nil {
			return errors.New(i18n.T(i18n.EmbedBundleMiss, asset.Rel))
		}
		source = flat
	}
	return copyStream(source, target)
}

func copyStream(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	temporary := target + ".tmp"
	out, err := os.Create(temporary)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(temporary)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		os.Remove(temporary)
		return err
	}
	return nil
}

// fromNet 은 정해진 주소에서 받는다. **주소는 코드 상수다** — 어디서 받는지
// 사람이 코드를 보고 알 수 있어야 한다.
func fromNet(asset embed.Asset, target string) error {
	if asset.Name == embed.DLLFile {
		return fromZip(embed.RuntimeURL, embed.RuntimeInZip, target)
	}
	if embed.ReleaseBase == "" {
		return errors.New(i18n.T(i18n.EmbedNoSource))
	}
	return download(embed.ReleaseBase+asset.Rel, target)
}

func download(url, target string) error {
	client := &http.Client{Timeout: fetchTimeout}
	answer, err := client.Get(url)
	if err != nil {
		return err
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		return errors.New(url + " : " + answer.Status)
	}
	temporary := target + ".tmp"
	out, err := os.Create(temporary)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, answer.Body); err != nil {
		out.Close()
		os.Remove(temporary)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		os.Remove(temporary)
		return err
	}
	return nil
}

// fromZip 은 zip 을 받아 그 안의 파일 하나만 꺼낸다. zip 자체는 자리에 안 남긴다.
func fromZip(url, member, target string) error {
	holder := target + ".zip"
	if err := download(url, holder); err != nil {
		return err
	}
	defer os.Remove(holder)
	reader, err := zip.OpenReader(holder)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, entry := range reader.File {
		if entry.Name != member {
			continue
		}
		in, err := entry.Open()
		if err != nil {
			return err
		}
		defer in.Close()
		temporary := target + ".tmp"
		out, err := os.Create(temporary)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			os.Remove(temporary)
			return err
		}
		if err := out.Close(); err != nil {
			os.Remove(temporary)
			return err
		}
		return os.Rename(temporary, target)
	}
	return errors.New(i18n.T(i18n.EmbedBundleMiss, member))
}

// EmbedCheck 는 `mem install --check` 다. 아무것도 안 고치고 무엇이 갖춰졌는지만
// 말한다. 여기서는 **박아 둔 SHA 까지 다 재 본다** — 싼 검사로는 깨진 파일을
// 못 잡는다.
func EmbedCheck(model string) (*Report, bool) {
	if model == "" {
		model = embed.DefaultModel
	}
	report := &Report{}
	runtimeAsset := embed.RuntimeAsset()
	report.add(Step{What: i18n.T(i18n.EmbedStepRuntime),
		Now:  deepState(embed.DLLPath(), runtimeAsset),
		Todo: i18n.T(i18n.EmbedTodoKeep)})
	for _, asset := range embed.ModelAssets(model) {
		report.add(Step{What: asset.Name,
			Now:  deepState(filepath.Join(embed.ModelDir(model), asset.Name), asset),
			Todo: i18n.T(i18n.EmbedTodoKeep)})
	}
	_, ok := embed.CheckDeep(model)
	if ok {
		report.note(i18n.T(i18n.EmbedDone, model))
	} else {
		report.note(i18n.T(i18n.EmbedNeedsInstall))
	}
	return report, ok
}

func deepState(path string, asset embed.Asset) string {
	if _, err := os.Stat(path); err != nil {
		return i18n.T(i18n.EmbedStateNone)
	}
	if !embed.SHAOK(path, asset.SHA) {
		return i18n.T(i18n.EmbedStateBroken)
	}
	return i18n.T(i18n.EmbedStateOK)
}

// embedCheck 는 `mem status --doctor` 의 임베딩 줄이다. 꺼져 있으면 **크게**
// 알린다 — 낱말 모드로 조용히 도는 것이 제일 나쁘다 (사용자 결정).
func embedCheck() Check {
	name := embed.DefaultModel
	missing, ok := embed.CheckDeep(name)
	if ok {
		return Check{What: i18n.T(i18n.EmbedStepModel), OK: true,
			Note: i18n.T(i18n.EmbedDone, name)}
	}
	return Check{What: i18n.T(i18n.EmbedStepModel), OK: false,
		Note: i18n.T(i18n.EmbedNeedsInstall) + " (" + joinLines(missing) + ")"}
}
