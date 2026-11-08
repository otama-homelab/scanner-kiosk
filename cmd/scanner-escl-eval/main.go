package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"scanner-kiosk/internal/esclcapture"
)

const receiptFormat = "scanner-escl-input-comparison"

const (
	targetFormat   = "scanner-escl-input-target"
	evidenceFormat = "scanner-escl-input-comparison-evidence"
	maxReceipts    = 90
	maxImageBytes  = 64 << 20
	maxImagePixels = 80_000_000
)

type receipt struct {
	Format        string             `json:"format"`
	Version       int                `json:"version"`
	CreatedAt     string             `json:"created_at"`
	Arm           string             `json:"arm"`
	Intent        string             `json:"intent,omitempty"`
	Region        esclcapture.Region `json:"region"`
	Resolution    int                `json:"resolution"`
	Color         string             `json:"color"`
	MediaType     string             `json:"media_type"`
	Bytes         int64              `json:"bytes"`
	SHA256        string             `json:"sha256"`
	Width         int                `json:"width"`
	Height        int                `json:"height"`
	ElapsedMillis int64              `json:"elapsed_millis"`
	Image         string             `json:"image"`
	TargetSHA256  string             `json:"target_sha256"`
	Device        string             `json:"device"`
	Firmware      string             `json:"firmware"`
	Sequence      int                `json:"sequence"`
}

type targetOracle struct {
	Format         string             `json:"format"`
	Version        int                `json:"version"`
	TargetSHA256   string             `json:"target_sha256"`
	Device         string             `json:"device"`
	Firmware       string             `json:"firmware"`
	Region         esclcapture.Region `json:"region"`
	Resolution     int                `json:"resolution"`
	Color          string             `json:"color"`
	BackgroundRGB  [3]uint8           `json:"background_rgb"`
	EdgeRegions    []normalizedRegion `json:"edge_regions"`
	ContentRegions []normalizedRegion `json:"content_regions"`
}

type normalizedRegion struct {
	ID     string  `json:"id"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type captureEvidence struct {
	Sequence         int              `json:"sequence"`
	MediaType        string           `json:"media_type"`
	SHA256           string           `json:"sha256"`
	Bytes            int64            `json:"bytes"`
	ElapsedMillis    int64            `json:"elapsed_millis"`
	Width            int              `json:"width"`
	Height           int              `json:"height"`
	ExpectedWidth    int              `json:"expected_width"`
	ExpectedHeight   int              `json:"expected_height"`
	CropWidthPixels  int              `json:"crop_width_pixels"`
	CropHeightPixels int              `json:"crop_height_pixels"`
	ScaleX           float64          `json:"scale_x"`
	ScaleY           float64          `json:"scale_y"`
	RotationDegrees  *int             `json:"rotation_degrees"`
	Regions          []regionEvidence `json:"regions"`
}

type regionEvidence struct {
	ID             string  `json:"id"`
	Kind           string  `json:"kind"`
	Foreground     int64   `json:"foreground_pixels"`
	RetentionRatio float64 `json:"retention_ratio"`
}

type armEvidence struct {
	Arm          string            `json:"arm"`
	CaptureCount int               `json:"capture_count"`
	Captures     []captureEvidence `json:"captures"`
	Regions      []regionEvidence  `json:"regions"`
	ElapsedP50   int64             `json:"elapsed_p50_millis"`
	ElapsedP95   int64             `json:"elapsed_p95_millis"`
}

type sequenceGroup struct {
	Group     int      `json:"group"`
	Sequences []int    `json:"sequences"`
	Arms      []string `json:"arms"`
}

type evidence struct {
	Format         string             `json:"format"`
	Version        int                `json:"version"`
	Status         string             `json:"status"`
	TargetSHA256   string             `json:"target_sha256"`
	Device         string             `json:"device"`
	Firmware       string             `json:"firmware"`
	Region         esclcapture.Region `json:"region"`
	Resolution     int                `json:"resolution"`
	Color          string             `json:"color"`
	SequenceGroups []sequenceGroup    `json:"sequence_groups,omitempty"`
	Arms           []armEvidence      `json:"arms"`
	Issues         []string           `json:"issues,omitempty"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "scanner-escl-eval: invalid input")
		return 2
	}
	switch args[0] {
	case "capture":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		endpoint := flags.String("endpoint", "", "")
		arm := flags.String("arm", "", "")
		intent := flags.String("intent", "", "")
		regionText := flags.String("region", "", "")
		resolution := flags.Int("resolution", 300, "")
		color := flags.String("color", "rgb", "")
		targetPath := flags.String("target", "", "")
		sequence := flags.Int("sequence", 0, "")
		output := flags.String("output", "", "")
		receiptPath := flags.String("receipt", "", "")
		if flags.Parse(args[1:]) != nil || *endpoint == "" || *arm != "e1" && *arm != "e2" ||
			*arm == "e1" && *intent != "" || *arm == "e2" && *intent == "" ||
			*targetPath == "" || *sequence < 1 || *output == "" || *receiptPath == "" || len(flags.Args()) != 0 {
			fmt.Fprintln(stderr, "scanner-escl-eval: invalid input")
			return 2
		}
		region, err := parseRegion(*regionText)
		target, targetErr := readTarget(*targetPath)
		if err != nil || targetErr != nil || region != target.Region || *resolution != target.Resolution || *color != target.Color {
			fmt.Fprintln(stderr, "scanner-escl-eval: invalid input")
			return 2
		}
		client, err := esclcapture.New(esclcapture.Options{
			Endpoint: *endpoint, Region: region, Resolution: *resolution, Color: *color, Intent: *intent,
			Timeout: 60 * time.Second, MaxResponseBytes: 16 << 20, MaxImageBytes: 64 << 20, MaxPixels: 80_000_000,
		})
		if err != nil {
			fmt.Fprintln(stderr, "scanner-escl-eval: unavailable")
			return 1
		}
		if err := os.Mkdir(*output, 0o700); err != nil {
			fmt.Fprintln(stderr, "scanner-escl-eval: output unavailable")
			return 1
		}
		result, err := client.Capture(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "scanner-escl-eval: unavailable")
			return 1
		}
		if err := os.WriteFile(filepath.Join(*output, "capture.png"), result.Image, 0o600); err != nil {
			fmt.Fprintln(stderr, "scanner-escl-eval: output unavailable")
			return 1
		}
		imagePath, err := receiptImageReference(*receiptPath, filepath.Join(*output, "capture.png"))
		if err != nil {
			fmt.Fprintln(stderr, "scanner-escl-eval: output unavailable")
			return 1
		}
		value := receipt{
			Format: receiptFormat, Version: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Arm: result.Arm, Intent: result.Intent, Region: result.Region, Resolution: result.Resolution,
			Color: result.Color, MediaType: result.MediaType, Bytes: result.Bytes, SHA256: result.SHA256,
			Width: result.Width, Height: result.Height, ElapsedMillis: result.ElapsedMillis, Image: imagePath,
			TargetSHA256: target.TargetSHA256, Device: target.Device, Firmware: target.Firmware, Sequence: *sequence,
		}
		if err := writeJSONExclusive(*receiptPath, value); err != nil {
			fmt.Fprintln(stderr, "scanner-escl-eval: output unavailable")
			return 1
		}
		_ = json.NewEncoder(stdout).Encode(value)
		return 0
	case "evaluate-c1":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		input := flags.String("input", "", "")
		regionText := flags.String("region", "", "")
		resolution := flags.Int("resolution", 300, "")
		color := flags.String("color", "rgb", "")
		targetPath := flags.String("target", "", "")
		sequence := flags.Int("sequence", 0, "")
		elapsedMillis := flags.Int64("elapsed-millis", 0, "")
		receiptPath := flags.String("receipt", "", "")
		if flags.Parse(args[1:]) != nil || *input == "" || *targetPath == "" || *sequence < 1 ||
			*elapsedMillis < 1 || *receiptPath == "" || len(flags.Args()) != 0 {
			fmt.Fprintln(stderr, "scanner-escl-eval: invalid input")
			return 2
		}
		region, err := parseRegion(*regionText)
		target, targetErr := readTarget(*targetPath)
		if err != nil || targetErr != nil || region != target.Region || *resolution != target.Resolution || *color != target.Color {
			fmt.Fprintln(stderr, "scanner-escl-eval: invalid input")
			return 2
		}
		body, width, height, err := readPNG(*input)
		if err != nil {
			fmt.Fprintln(stderr, "scanner-escl-eval: unavailable")
			return 1
		}
		imagePath, err := receiptImageReference(*receiptPath, *input)
		if err != nil {
			fmt.Fprintln(stderr, "scanner-escl-eval: unavailable")
			return 1
		}
		sum := sha256.Sum256(body)
		value := receipt{
			Format: receiptFormat, Version: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Arm: "c1", Region: region, Resolution: *resolution, Color: *color,
			MediaType: "image/png", Bytes: int64(len(body)), SHA256: hex.EncodeToString(sum[:]),
			Width: width, Height: height, ElapsedMillis: *elapsedMillis, Image: imagePath,
			TargetSHA256: target.TargetSHA256, Device: target.Device, Firmware: target.Firmware, Sequence: *sequence,
		}
		if err := writeJSONExclusive(*receiptPath, value); err != nil {
			fmt.Fprintln(stderr, "scanner-escl-eval: output unavailable")
			return 1
		}
		_ = json.NewEncoder(stdout).Encode(value)
		return 0
	case "compare":
		flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		var paths stringList
		flags.Var(&paths, "receipt", "")
		targetPath := flags.String("target", "", "")
		if flags.Parse(args[1:]) != nil || *targetPath == "" || len(paths) == 0 ||
			len(paths) > maxReceipts || len(flags.Args()) != 0 {
			fmt.Fprintln(stderr, "scanner-escl-eval: invalid input")
			return 2
		}
		target, err := readTarget(*targetPath)
		if err != nil {
			fmt.Fprintln(stderr, "scanner-escl-eval: invalid input")
			return 2
		}
		value := compareReceipts(target, paths)
		_ = json.NewEncoder(stdout).Encode(value)
		if value.Status != "complete" {
			return 1
		}
		return 0
	default:
		fmt.Fprintln(stderr, "scanner-escl-eval: invalid input")
		return 2
	}
}

func parseRegion(value string) (esclcapture.Region, error) {
	fields := strings.Split(value, ",")
	if len(fields) != 4 {
		return esclcapture.Region{}, errors.New("invalid region")
	}
	numbers := make([]int, 4)
	for index, field := range fields {
		number, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || number < 0 {
			return esclcapture.Region{}, errors.New("invalid region")
		}
		numbers[index] = number
	}
	if numbers[2] < 1 || numbers[3] < 1 {
		return esclcapture.Region{}, errors.New("invalid region")
	}
	return esclcapture.Region{X: numbers[0], Y: numbers[1], Width: numbers[2], Height: numbers[3]}, nil
}

func readPNG(path string) ([]byte, int, int, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxImageBytes {
		return nil, 0, 0, esclcapture.ErrInvalid
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, 0, esclcapture.ErrInvalid
	}
	decoded, err := png.Decode(bytes.NewReader(body))
	if err != nil || decoded.Bounds().Dx() < 1 || decoded.Bounds().Dy() < 1 ||
		int64(decoded.Bounds().Dx()) > maxImagePixels/int64(decoded.Bounds().Dy()) {
		return nil, 0, 0, esclcapture.ErrInvalid
	}
	return body, decoded.Bounds().Dx(), decoded.Bounds().Dy(), nil
}

func compareReceipts(target targetOracle, paths []string) evidence {
	value := evidence{
		Format: evidenceFormat, Version: 1, Status: "complete",
		TargetSHA256: target.TargetSHA256, Device: target.Device, Firmware: target.Firmware,
		Region: target.Region, Resolution: target.Resolution, Color: target.Color,
		Arms: []armEvidence{},
	}
	issue := func(message string) { value.Issues = append(value.Issues, message) }
	seenPaths := map[string]bool{}
	seenSequences := map[int]bool{}
	seenImages := map[string]bool{}
	byArm := map[string][]captureEvidence{}
	var all []struct {
		sequence int
		arm      string
	}
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil || seenPaths[absolute] {
			issue("duplicate or invalid receipt path")
			continue
		}
		seenPaths[absolute] = true
		item, err := readReceipt(path)
		if err != nil {
			issue("receipt cannot be read")
			continue
		}
		if message := receiptProblem(item, target); message != "" {
			issue(message)
			continue
		}
		if seenSequences[item.Sequence] {
			issue("sequence values must be globally unique")
			continue
		}
		seenSequences[item.Sequence] = true
		imageKey := filepath.Join(filepath.Dir(absolute), filepath.FromSlash(item.Image))
		if seenImages[imageKey] {
			issue("receipt image references must be unique")
			continue
		}
		seenImages[imageKey] = true
		bitmap, err := readReceiptImage(path, item.Image)
		if err != nil || bitmap.Bounds().Dx() != item.Width || bitmap.Bounds().Dy() != item.Height {
			issue("receipt image is not a matching bounded PNG")
			continue
		}
		regions := measureTargetRegions(bitmap, target)
		capture := captureEvidence{
			Sequence: item.Sequence, MediaType: item.MediaType, SHA256: item.SHA256,
			Bytes: item.Bytes, ElapsedMillis: item.ElapsedMillis, Width: item.Width, Height: item.Height,
			Regions: regions,
		}
		setGeometry(&capture, item)
		if item.Arm == "c1" {
			for _, region := range regions {
				if region.Foreground == 0 {
					issue("C1 oracle region has no foreground: " + region.ID)
				}
			}
		}
		byArm[item.Arm] = append(byArm[item.Arm], capture)
		all = append(all, struct {
			sequence int
			arm      string
		}{item.Sequence, item.Arm})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].sequence < all[j].sequence })
	if len(all) < 9 || len(all)%3 != 0 {
		issue("complete evidence requires at least three ordered C1/E1/E2 groups")
	}
	for begin := 0; begin+3 <= len(all); begin += 3 {
		group := sequenceGroup{Group: begin/3 + 1}
		arms := map[string]bool{}
		for _, item := range all[begin : begin+3] {
			group.Sequences = append(group.Sequences, item.sequence)
			group.Arms = append(group.Arms, item.arm)
			arms[item.arm] = true
		}
		if !arms["c1"] || !arms["e1"] || !arms["e2"] {
			issue("each sequence group must contain C1, E1 and E2")
		}
		value.SequenceGroups = append(value.SequenceGroups, group)
	}
	targetRegions := append(append([]normalizedRegion(nil), target.EdgeRegions...), target.ContentRegions...)
	c1Regions := map[string]int64{}
	for _, capture := range byArm["c1"] {
		for _, region := range capture.Regions {
			c1Regions[region.ID] += region.Foreground
		}
	}
	for _, arm := range []string{"c1", "e1", "e2"} {
		captures := byArm[arm]
		if len(captures) < 3 {
			issue(arm + " requires at least three captures")
		}
		sort.Slice(captures, func(i, j int) bool { return captures[i].Sequence < captures[j].Sequence })
		arms := armEvidence{Arm: arm, CaptureCount: len(captures), Captures: captures}
		foreground := map[string]int64{}
		var timings []int64
		for _, capture := range captures {
			timings = append(timings, capture.ElapsedMillis)
			for _, region := range capture.Regions {
				foreground[region.ID] += region.Foreground
			}
		}
		for _, region := range targetRegions {
			measurement := regionEvidence{ID: region.ID, Kind: regionKind(target, region.ID)}
			measurement.Foreground = foreground[region.ID]
			if baseline := c1Regions[region.ID]; baseline > 0 {
				measurement.RetentionRatio = roundRatio(float64(measurement.Foreground) / float64(baseline))
			} else {
				issue("C1 baseline region has no foreground: " + region.ID)
			}
			arms.Regions = append(arms.Regions, measurement)
		}
		if len(timings) > 0 {
			arms.ElapsedP50 = nearestRank(timings, 50)
			arms.ElapsedP95 = nearestRank(timings, 95)
		}
		value.Arms = append(value.Arms, arms)
	}
	if len(value.Issues) != 0 {
		value.Status = "incomplete"
	}
	return value
}

func receiptProblem(value receipt, target targetOracle) string {
	if value.Format != receiptFormat || value.Version != 1 || value.CreatedAt == "" ||
		(value.Arm != "c1" && value.Arm != "e1" && value.Arm != "e2") ||
		value.Arm == "e1" && value.Intent != "" || value.Arm == "e2" && !validLabel(value.Intent) ||
		value.Arm == "c1" && value.Intent != "" ||
		value.Sequence < 1 || value.ElapsedMillis < 1 || value.Bytes < 1 ||
		value.Width < 1 || value.Height < 1 || !validSHA256(value.SHA256) ||
		(value.MediaType != "image/png" && value.MediaType != "image/jpeg") ||
		!safeImageReference(value.Image) {
		return "receipt metadata is invalid"
	}
	if value.TargetSHA256 != target.TargetSHA256 || value.Device != target.Device ||
		value.Firmware != target.Firmware || value.Region != target.Region ||
		value.Resolution != target.Resolution || value.Color != target.Color {
		return "receipt target, device, firmware or requested settings do not match the oracle"
	}
	return ""
}

func readTarget(path string) (targetOracle, error) {
	var value targetOracle
	if err := readStrictJSON(path, 64<<10, &value); err != nil ||
		value.Format != targetFormat || value.Version != 1 || !validSHA256(value.TargetSHA256) ||
		!validLabel(value.Device) || !validLabel(value.Firmware) ||
		value.Region.X < 0 || value.Region.Y < 0 || value.Region.Width < 1 || value.Region.Height < 1 ||
		value.Resolution < 75 || value.Resolution > 1200 ||
		(value.Color != "rgb" && value.Color != "gray") ||
		len(value.EdgeRegions) < 1 || len(value.EdgeRegions) > 32 ||
		len(value.ContentRegions) < 1 || len(value.ContentRegions) > 32 {
		return targetOracle{}, esclcapture.ErrInvalid
	}
	seen := map[string]bool{}
	for _, region := range append(append([]normalizedRegion(nil), value.EdgeRegions...), value.ContentRegions...) {
		if !validLabel(region.ID) || seen[region.ID] || !validNormalizedRegion(region) {
			return targetOracle{}, esclcapture.ErrInvalid
		}
		seen[region.ID] = true
	}
	return value, nil
}

func validNormalizedRegion(region normalizedRegion) bool {
	values := []float64{region.X, region.Y, region.Width, region.Height}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return region.X >= 0 && region.Y >= 0 && region.Width > 0 && region.Height > 0 &&
		region.X+region.Width <= 1 && region.Y+region.Height <= 1
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validLabel(value string) bool {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func receiptImageReference(receiptPath, imagePath string) (string, error) {
	receiptAbsolute, err := filepath.Abs(receiptPath)
	if err != nil {
		return "", esclcapture.ErrInvalid
	}
	imageAbsolute, err := filepath.Abs(imagePath)
	if err != nil {
		return "", esclcapture.ErrInvalid
	}
	relative, err := filepath.Rel(filepath.Dir(receiptAbsolute), imageAbsolute)
	relative = filepath.ToSlash(relative)
	if err != nil || !safeImageReference(relative) {
		return "", esclcapture.ErrInvalid
	}
	return relative, nil
}

func safeImageReference(value string) bool {
	return value != "" && value != "." && !filepath.IsAbs(value) &&
		filepath.ToSlash(filepath.Clean(filepath.FromSlash(value))) == value &&
		value != ".." && !strings.HasPrefix(value, "../") && !strings.Contains(value, "/../")
}

func readReceiptImage(receiptPath, imageReference string) (image.Image, error) {
	receiptAbsolute, err := filepath.Abs(receiptPath)
	if err != nil {
		return nil, esclcapture.ErrInvalid
	}
	root, err := os.OpenRoot(filepath.Dir(receiptAbsolute))
	if err != nil {
		return nil, esclcapture.ErrInvalid
	}
	defer root.Close()
	current := ""
	parts := strings.Split(imageReference, "/")
	var info os.FileInfo
	for index, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, esclcapture.ErrInvalid
		}
		current = filepath.ToSlash(filepath.Join(current, part))
		info, err = root.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 ||
			index < len(parts)-1 && !info.IsDir() || index == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, esclcapture.ErrInvalid
		}
	}
	if info.Size() < 1 || info.Size() > maxImageBytes {
		return nil, esclcapture.ErrInvalid
	}
	file, err := root.Open(imageReference)
	if err != nil {
		return nil, esclcapture.ErrInvalid
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxImageBytes+1))
	if err != nil || int64(len(body)) > maxImageBytes {
		return nil, esclcapture.ErrInvalid
	}
	config, err := png.DecodeConfig(bytes.NewReader(body))
	if err != nil || config.Width < 1 || config.Height < 1 ||
		int64(config.Width) > maxImagePixels/int64(config.Height) {
		return nil, esclcapture.ErrInvalid
	}
	bitmap, err := png.Decode(bytes.NewReader(body))
	if err != nil || bitmap.Bounds().Dx() != config.Width || bitmap.Bounds().Dy() != config.Height {
		return nil, esclcapture.ErrInvalid
	}
	return bitmap, nil
}

func measureTargetRegions(bitmap image.Image, target targetOracle) []regionEvidence {
	var output []regionEvidence
	for _, item := range []struct {
		kind    string
		regions []normalizedRegion
	}{{"edge", target.EdgeRegions}, {"content", target.ContentRegions}} {
		for _, region := range item.regions {
			output = append(output, regionEvidence{
				ID: region.ID, Kind: item.kind,
				Foreground: foregroundPixels(bitmap, region, target.BackgroundRGB),
			})
		}
	}
	return output
}

func foregroundPixels(bitmap image.Image, region normalizedRegion, background [3]uint8) int64 {
	bounds := bitmap.Bounds()
	x0 := int(math.Floor(region.X * float64(bounds.Dx())))
	y0 := int(math.Floor(region.Y * float64(bounds.Dy())))
	x1 := int(math.Ceil((region.X + region.Width) * float64(bounds.Dx())))
	y1 := int(math.Ceil((region.Y + region.Height) * float64(bounds.Dy())))
	var count int64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, g, b, _ := bitmap.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			if channelDistance(uint8(r>>8), background[0])+
				channelDistance(uint8(g>>8), background[1])+
				channelDistance(uint8(b>>8), background[2]) > 24 {
				count++
			}
		}
	}
	return count
}

func channelDistance(a, b uint8) int {
	value := int(a) - int(b)
	if value < 0 {
		return -value
	}
	return value
}

func setGeometry(capture *captureEvidence, item receipt) {
	expectedWidth := (item.Region.Width*item.Resolution + 299) / 300
	expectedHeight := (item.Region.Height*item.Resolution + 299) / 300
	capture.ExpectedWidth = expectedWidth
	capture.ExpectedHeight = expectedHeight
	capture.CropWidthPixels = expectedWidth - item.Width
	capture.CropHeightPixels = expectedHeight - item.Height
	capture.ScaleX = roundRatio(float64(item.Width) / float64(expectedWidth))
	capture.ScaleY = roundRatio(float64(item.Height) / float64(expectedHeight))
	if expectedWidth != expectedHeight {
		rotation := -1
		if item.Width == expectedWidth && item.Height == expectedHeight {
			rotation = 0
		} else if item.Width == expectedHeight && item.Height == expectedWidth {
			rotation = 90
		}
		if rotation >= 0 {
			capture.RotationDegrees = &rotation
		}
	}
}

func regionKind(target targetOracle, id string) string {
	for _, region := range target.EdgeRegions {
		if region.ID == id {
			return "edge"
		}
	}
	return "content"
}

func roundRatio(value float64) float64 {
	return math.Round(value*1_000_000) / 1_000_000
}

func nearestRank(values []int64, percentile int) int64 {
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	rank := (percentile*len(sorted) + 99) / 100
	return sorted[rank-1]
}

func writeJSONExclusive(path string, value any) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return esclcapture.ErrInvalid
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(body, '\n')); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func readReceipt(path string) (receipt, error) {
	var value receipt
	if err := readStrictJSON(path, 1<<20, &value); err != nil {
		return receipt{}, esclcapture.ErrInvalid
	}
	return value, nil
}

func readStrictJSON(path string, maxBytes int64, value any) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxBytes {
		return esclcapture.ErrInvalid
	}
	file, err := os.Open(path)
	if err != nil {
		return esclcapture.ErrInvalid
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return esclcapture.ErrInvalid
	}
	return nil
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}
