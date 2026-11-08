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
	"image/png"
	"io"
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
		output := flags.String("output", "", "")
		receiptPath := flags.String("receipt", "", "")
		if flags.Parse(args[1:]) != nil || *endpoint == "" || *arm != "e1" && *arm != "e2" ||
			*arm == "e1" && *intent != "" || *arm == "e2" && *intent == "" ||
			*output == "" || *receiptPath == "" || len(flags.Args()) != 0 {
			fmt.Fprintln(stderr, "scanner-escl-eval: invalid input")
			return 2
		}
		region, err := parseRegion(*regionText)
		if err != nil {
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
		value := receipt{
			Format: receiptFormat, Version: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Arm: result.Arm, Intent: result.Intent, Region: result.Region, Resolution: result.Resolution,
			Color: result.Color, MediaType: result.MediaType, Bytes: result.Bytes, SHA256: result.SHA256,
			Width: result.Width, Height: result.Height, ElapsedMillis: result.ElapsedMillis, Image: "capture.png",
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
		receiptPath := flags.String("receipt", "", "")
		if flags.Parse(args[1:]) != nil || *input == "" || *receiptPath == "" || len(flags.Args()) != 0 {
			fmt.Fprintln(stderr, "scanner-escl-eval: invalid input")
			return 2
		}
		region, err := parseRegion(*regionText)
		if err != nil {
			fmt.Fprintln(stderr, "scanner-escl-eval: invalid input")
			return 2
		}
		body, width, height, err := readPNG(*input)
		if err != nil {
			fmt.Fprintln(stderr, "scanner-escl-eval: unavailable")
			return 1
		}
		sum := sha256.Sum256(body)
		value := receipt{
			Format: receiptFormat, Version: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Arm: "c1", Region: region, Resolution: *resolution, Color: *color,
			MediaType: "image/png", Bytes: int64(len(body)), SHA256: hex.EncodeToString(sum[:]),
			Width: width, Height: height, Image: filepath.Base(*input),
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
		if flags.Parse(args[1:]) != nil || len(paths) == 0 || len(flags.Args()) != 0 {
			fmt.Fprintln(stderr, "scanner-escl-eval: invalid input")
			return 2
		}
		var values []receipt
		seen := map[string]bool{}
		for _, path := range paths {
			value, err := readReceipt(path)
			if err != nil || value.Format != receiptFormat || value.Version != 1 || seen[value.Arm+value.SHA256] {
				fmt.Fprintln(stderr, "scanner-escl-eval: unavailable")
				return 1
			}
			seen[value.Arm+value.SHA256] = true
			values = append(values, value)
		}
		sort.Slice(values, func(i, j int) bool {
			if values[i].Arm != values[j].Arm {
				return values[i].Arm < values[j].Arm
			}
			return values[i].CreatedAt < values[j].CreatedAt
		})
		_ = json.NewEncoder(stdout).Encode(map[string]any{
			"format": "scanner-escl-input-comparison-result", "version": 1, "receipts": values,
		})
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
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 64<<20 {
		return nil, 0, 0, esclcapture.ErrInvalid
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, 0, esclcapture.ErrInvalid
	}
	decoded, err := png.Decode(bytes.NewReader(body))
	if err != nil || decoded.Bounds().Dx() < 1 || decoded.Bounds().Dy() < 1 ||
		int64(decoded.Bounds().Dx()) > 80_000_000/int64(decoded.Bounds().Dy()) {
		return nil, 0, 0, esclcapture.ErrInvalid
	}
	return body, decoded.Bounds().Dx(), decoded.Bounds().Dy(), nil
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
	file, err := os.Open(path)
	if err != nil {
		return receipt{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var value receipt
	if err := decoder.Decode(&value); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return receipt{}, esclcapture.ErrInvalid
	}
	return value, nil
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}
