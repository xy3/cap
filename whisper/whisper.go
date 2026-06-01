package whisper

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sashabaranov/go-openai"
	ffmpeg "github.com/u2takey/ffmpeg-go"
)

type Word struct {
	Text  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

type TranscriptionResult struct {
	Words    []Word  `json:"words"`
	FullText string  `json:"text"`
	Duration float64 `json:"duration"`
}

type Transcriber interface {
	Transcribe(ctx context.Context, videoPath string, params Params) (*TranscriptionResult, error)
}

type Params struct {
	Model       string
	Language    string
	Prompt      string
	Temperature float32
	BinaryPath  string
	ModelPath   string
}

type OpenAIClient struct {
	apiKey string
	client *openai.Client
}

func NewOpenAIClient(apiKey string) *OpenAIClient {
	return &OpenAIClient{
		apiKey: apiKey,
		client: openai.NewClient(apiKey),
	}
}

func (c *OpenAIClient) Transcribe(ctx context.Context, videoPath string, params Params) (*TranscriptionResult, error) {
	audioPath, err := extractAudio(videoPath)
	if err != nil {
		return nil, fmt.Errorf("extracting audio: %w", err)
	}
	defer os.Remove(audioPath)

	req := openai.AudioRequest{
		Model:       params.Model,
		FilePath:    audioPath,
		Format:      openai.AudioResponseFormatVerboseJSON,
		TimestampGranularities: []openai.TranscriptionTimestampGranularity{
			openai.TranscriptionTimestampGranularityWord,
		},
		Temperature: params.Temperature,
	}

	if params.Language != "" {
		req.Language = params.Language
	}
	if params.Prompt != "" {
		req.Prompt = params.Prompt
	}

	resp, err := c.client.CreateTranscription(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("whisper API call: %w", err)
	}

	result := &TranscriptionResult{
		FullText: resp.Text,
		Duration: resp.Duration,
	}

	for _, w := range resp.Words {
		result.Words = append(result.Words, Word{
			Text:  w.Word,
			Start: w.Start,
			End:   w.End,
		})
	}

	return result, nil
}

type LocalClient struct {
	binaryPath string
	modelPath  string
	language   string
}

func NewLocalClient(binaryPath, modelPath, language string) *LocalClient {
	return &LocalClient{
		binaryPath: binaryPath,
		modelPath:  modelPath,
		language:   language,
	}
}

func (c *LocalClient) Transcribe(ctx context.Context, videoPath string, params Params) (*TranscriptionResult, error) {
	audioPath, err := extractAudio(videoPath)
	if err != nil {
		return nil, fmt.Errorf("extracting audio: %w", err)
	}
	defer os.Remove(audioPath)

	outDir, err := os.MkdirTemp("", "capper_whisper_")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(outDir)

	outPrefix := filepath.Join(outDir, "whisper_out")

	binary := c.binaryPath
	model := c.modelPath
	language := params.Language
	if language == "" {
		language = c.language
	}

	if isWhisperCPP(binary) {
		return c.runWhisperCPP(ctx, binary, model, audioPath, language, outPrefix)
	}

	return c.runPythonWhisper(ctx, binary, model, audioPath, language, outPrefix)
}

func (c *LocalClient) runPythonWhisper(ctx context.Context, binary, model, audioPath, language, outPrefix string) (*TranscriptionResult, error) {
	outDir := filepath.Dir(outPrefix)

	// First attempt: GPU/CUDA with word-level timestamps
	args := []string{
		audioPath,
		"--model", model,
		"--output_dir", outDir,
		"--output_format", "json",
		"--word_timestamps", "True",
		"--fp16", "False",
	}

	if language != "" {
		args = append(args, "--language", language)
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
	jsonPath := findWhisperOutput(outDir, ".json")

	// word_timestamps on GPU can fail silently (whisper catches triton errors and exits 0)
	// Retry on CPU
	if jsonPath == "" {
		fmt.Println("Retrying with word-level timestamps on CPU...")

		args2 := append([]string{}, args...)
		args2 = append(args2, "--device", "cpu")
		cmd2 := exec.CommandContext(ctx, binary, args2...)
		cmd2.Stdout = os.Stdout
		cmd2.Stderr = os.Stderr
		_ = cmd2.Run()
		jsonPath = findWhisperOutput(outDir, ".json")
	}

	// If word timestamps still fail, fallback to segment-level timestamps
	if jsonPath == "" {
		fmt.Println("Retrying with segment-level timestamps (no word alignment)...")

		args3 := []string{
			audioPath,
			"--model", model,
			"--output_dir", outDir,
			"--output_format", "json",
			"--fp16", "False",
		}
		if language != "" {
			args3 = append(args3, "--language", language)
		}

		cmd3 := exec.CommandContext(ctx, binary, args3...)
		cmd3.Stdout = os.Stdout
		cmd3.Stderr = os.Stderr
		if err := cmd3.Run(); err != nil {
			return nil, fmt.Errorf("running local whisper: %w", err)
		}
		jsonPath = findWhisperOutput(outDir, ".json")
	}

	if jsonPath == "" {
		return nil, fmt.Errorf("whisper output file not found in %s", outDir)
	}

	return parsePythonWhisperJSON(jsonPath)
}

func (c *LocalClient) runWhisperCPP(ctx context.Context, binary, model, audioPath, language, outPrefix string) (*TranscriptionResult, error) {
	args := []string{
		"-m", model,
		"-f", audioPath,
		"-oj",
		"-ml", "1",
		"-of", outPrefix,
	}

	if language != "" && language != "en" {
		args = append(args, "-l", language)
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("running whisper.cpp: %w", err)
	}

	jsonPath := outPrefix + ".json"
	return parseWhisperCPPJSON(jsonPath)
}

func isWhisperCPP(binary string) bool {
	base := filepath.Base(binary)
	// Common whisper.cpp binary names
	return base == "whisper-cli" || base == "whisper.cpp" || base == "main" || base == "whisper-cpp"
}

func findWhisperOutput(dir, ext string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ext {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// --- Python openai-whisper JSON format ---

type pyWhisperOutput struct {
	Text     string       `json:"text"`
	Segments []pySegment  `json:"segments"`
}

type pySegment struct {
	Start float64   `json:"start"`
	End   float64   `json:"end"`
	Text  string    `json:"text"`
	Words []pyWord  `json:"words"`
}

type pyWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

func parsePythonWhisperJSON(path string) (*TranscriptionResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading whisper output: %w", err)
	}

	var out pyWhisperOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parsing whisper JSON: %w", err)
	}

	result := &TranscriptionResult{
		FullText: out.Text,
	}

	for _, seg := range out.Segments {
		if seg.End > result.Duration {
			result.Duration = seg.End
		}
		for _, w := range seg.Words {
			result.Words = append(result.Words, Word{
				Text:  w.Word,
				Start: w.Start,
				End:   w.End,
			})
		}
	}

	if len(result.Words) == 0 && len(out.Segments) > 0 {
		for _, seg := range out.Segments {
			if seg.End > result.Duration {
				result.Duration = seg.End
			}
			result.Words = append(result.Words, Word{
				Text:  seg.Text,
				Start: seg.Start,
				End:   seg.End,
			})
		}
	}

	return result, nil
}

// --- whisper.cpp JSON format (with -ml 1 each segment is one word) ---

type cppWhisperOutput struct {
	Transcription []cppSegment `json:"transcription"`
}

type cppSegment struct {
	Offsets    cppOffsets    `json:"offsets"`
	Timestamps cppTimestamps `json:"timestamps"`
	Text       string        `json:"text"`
}

type cppOffsets struct {
	From int `json:"from"`
	To   int `json:"to"`
}

type cppTimestamps struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func parseWhisperCPPJSON(path string) (*TranscriptionResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading whisper.cpp output: %w", err)
	}

	var out cppWhisperOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parsing whisper.cpp JSON: %w", err)
	}

	result := &TranscriptionResult{}

	for _, seg := range out.Transcription {
		start := float64(seg.Offsets.From) / 1000.0
		end := float64(seg.Offsets.To) / 1000.0

		if end > result.Duration {
			result.Duration = end
		}

		result.Words = append(result.Words, Word{
			Text:  seg.Text,
			Start: start,
			End:   end,
		})

		if result.FullText != "" {
			result.FullText += " "
		}
		result.FullText += seg.Text
	}

	return result, nil
}

// --- audio extraction ---

func extractAudio(videoPath string) (string, error) {
	ext := filepath.Ext(videoPath)
	base := videoPath[:len(videoPath)-len(ext)]

	outPath := base + "_audio.wav"

	err := ffmpeg.Input(videoPath).
		Output(outPath, ffmpeg.KwArgs{
			"vn":     "",
			"acodec": "pcm_s16le",
			"ar":     "16000",
			"ac":     "1",
		}).
		OverWriteOutput().
		Run()

	if err != nil {
		return "", fmt.Errorf("ffmpeg audio extraction: %w", err)
	}

	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		return "", fmt.Errorf("audio file was not created: %s", outPath)
	}

	return outPath, nil
}

// --- transcription cache ---

func CachePath(videoPath string) string {
	return videoPath + ".capper.json"
}

func SaveCache(videoPath string, result *TranscriptionResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(CachePath(videoPath), data, 0644)
}

func LoadCache(videoPath string) (*TranscriptionResult, error) {
	data, err := os.ReadFile(CachePath(videoPath))
	if err != nil {
		return nil, err
	}
	var result TranscriptionResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
