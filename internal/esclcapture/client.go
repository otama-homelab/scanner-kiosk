// Package esclcapture is a nonproduction, one-shot direct-eSCL experiment.
// It is never imported by product scanner admission.
package esclcapture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalid     = errors.New("invalid direct eSCL request")
	ErrUnavailable = errors.New("direct eSCL capture unavailable")
)

type Region struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Options struct {
	Endpoint                        string
	Region                          Region
	Resolution                      int
	Color                           string
	Intent                          string
	Timeout                         time.Duration
	MaxResponseBytes, MaxImageBytes int64
	MaxPixels                       int64
}

type Result struct {
	Arm           string `json:"arm"`
	Intent        string `json:"intent,omitempty"`
	Region        Region `json:"region"`
	Resolution    int    `json:"resolution"`
	Color         string `json:"color"`
	MediaType     string `json:"media_type"`
	Bytes         int64  `json:"bytes"`
	SHA256        string `json:"sha256"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	ElapsedMillis int64  `json:"elapsed_millis"`
	Image         []byte `json:"-"`
}

type Client struct {
	endpoint *url.URL
	options  Options
	http     *http.Client
}

func New(options Options) (*Client, error) {
	u, err := url.Parse(options.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" ||
		options.Region.X < 0 || options.Region.Y < 0 || options.Region.Width < 1 || options.Region.Height < 1 ||
		options.Resolution < 75 || options.Resolution > 1200 ||
		options.Color != "rgb" && options.Color != "gray" ||
		options.Timeout < time.Millisecond || options.Timeout > 5*time.Minute ||
		options.MaxResponseBytes < 1024 || options.MaxResponseBytes > 1<<30 ||
		options.MaxImageBytes < 1 || options.MaxImageBytes > 1<<30 ||
		options.MaxPixels < 1 || options.MaxPixels > 1<<40 ||
		options.Intent != "" && !token(options.Intent) {
		return nil, ErrInvalid
	}
	u.Path = strings.TrimRight(u.Path, "/")
	transport := &http.Transport{
		Proxy: nil, DialContext: (&net.Dialer{Timeout: options.Timeout}).DialContext,
		DisableKeepAlives: true, DisableCompression: true,
		TLSHandshakeTimeout: options.Timeout, ResponseHeaderTimeout: options.Timeout,
		MaxResponseHeaderBytes: 16 << 10,
	}
	httpClient := &http.Client{
		Transport: transport, Timeout: options.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrUnavailable },
	}
	return &Client{endpoint: u, options: options, http: httpClient}, nil
}

func (c *Client) Capture(ctx context.Context) (Result, error) {
	started := time.Now()
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	body, err := c.settings()
	if err != nil {
		return Result{}, err
	}
	jobURL := *c.endpoint
	jobURL.Path += "/ScanJobs"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, jobURL.String(), bytes.NewReader(body))
	if err != nil {
		return Result{}, ErrInvalid
	}
	request.Header.Set("Content-Type", "text/xml")
	request.Header.Set("Accept", "text/xml, application/xml")
	request.Header.Set("Accept-Encoding", "identity")
	response, err := c.http.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("create job: %w", unavailable(ctx, err))
	}
	location, locationErr := c.location(response)
	response.Body.Close()
	if locationErr != nil {
		return Result{}, fmt.Errorf("create job: HTTP %d or invalid location: %w", response.StatusCode, locationErr)
	}
	documentURL := *location
	documentURL.Path = strings.TrimRight(documentURL.Path, "/") + "/NextDocument"
	imageRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, documentURL.String(), nil)
	if err != nil {
		return Result{}, ErrInvalid
	}
	imageRequest.Header.Set("Accept", "image/jpeg")
	imageRequest.Header.Set("Accept-Encoding", "identity")
	imageResponse, err := c.http.Do(imageRequest)
	if err != nil {
		return Result{}, fmt.Errorf("read image: %w", unavailable(ctx, err))
	}
	defer imageResponse.Body.Close()
	if imageResponse.StatusCode != http.StatusOK || imageResponse.ContentLength > c.options.MaxImageBytes {
		return Result{}, fmt.Errorf("read image: HTTP %d or oversized response: %w", imageResponse.StatusCode, ErrUnavailable)
	}
	returnedMediaType := mediaType(imageResponse.Header.Get("Content-Type"))
	if returnedMediaType != "image/png" && returnedMediaType != "image/jpeg" {
		return Result{}, fmt.Errorf("read image: unsupported media type: %w", ErrUnavailable)
	}
	data, err := io.ReadAll(io.LimitReader(imageResponse.Body, c.options.MaxImageBytes+1))
	if err != nil || int64(len(data)) > c.options.MaxImageBytes {
		return Result{}, fmt.Errorf("read image: incomplete or oversized body: %w", ErrUnavailable)
	}
	normalized, width, height, err := normalizeImage(data, returnedMediaType, c.options.MaxPixels, c.options.MaxImageBytes)
	if err != nil {
		return Result{}, fmt.Errorf("decode image: %w", ErrInvalid)
	}
	deleteRequest, err := http.NewRequestWithContext(ctx, http.MethodDelete, location.String(), nil)
	if err != nil {
		return Result{}, ErrInvalid
	}
	deleteResponse, err := c.http.Do(deleteRequest)
	if err != nil {
		return Result{}, fmt.Errorf("delete job: %w", unavailable(ctx, err))
	}
	deleteResponse.Body.Close()
	// TS8130 removes completed jobs; the image has already been read and validated.
	if deleteResponse.StatusCode != http.StatusNotFound && (deleteResponse.StatusCode < 200 || deleteResponse.StatusCode >= 300) {
		return Result{}, fmt.Errorf("delete job: HTTP %d: %w", deleteResponse.StatusCode, ErrUnavailable)
	}
	sum := sha256.Sum256(data)
	arm := "e1"
	if c.options.Intent != "" {
		arm = "e2"
	}
	elapsedMillis := time.Since(started).Milliseconds()
	if elapsedMillis < 1 {
		elapsedMillis = 1
	}
	return Result{
		Arm: arm, Intent: c.options.Intent, Region: c.options.Region,
		Resolution: c.options.Resolution, Color: c.options.Color,
		MediaType: returnedMediaType, Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:]),
		Width: width, Height: height,
		ElapsedMillis: elapsedMillis, Image: normalized,
	}, nil
}

func normalizeImage(data []byte, returnedMediaType string, maxPixels, maxBytes int64) ([]byte, int, int, error) {
	var config image.Config
	var err error
	switch returnedMediaType {
	case "image/png":
		config, err = png.DecodeConfig(bytes.NewReader(data))
	case "image/jpeg":
		config, err = jpeg.DecodeConfig(bytes.NewReader(data))
	default:
		return nil, 0, 0, ErrInvalid
	}
	if err != nil || config.Width < 1 || config.Height < 1 ||
		int64(config.Width) > maxPixels/int64(config.Height) {
		return nil, 0, 0, ErrInvalid
	}
	var decoded image.Image
	switch returnedMediaType {
	case "image/png":
		decoded, err = png.Decode(bytes.NewReader(data))
	case "image/jpeg":
		decoded, err = jpeg.Decode(bytes.NewReader(data))
	}
	if err != nil || decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return nil, 0, 0, ErrInvalid
	}
	if returnedMediaType == "image/png" {
		return data, config.Width, config.Height, nil
	}
	var normalized bytes.Buffer
	if err := png.Encode(&normalized, decoded); err != nil || int64(normalized.Len()) > maxBytes {
		return nil, 0, 0, ErrInvalid
	}
	return normalized.Bytes(), config.Width, config.Height, nil
}

func (c *Client) location(response *http.Response) (*url.URL, error) {
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	values := response.Header.Values("Location")
	if len(values) != 1 || values[0] == "" || len(values[0]) > 4096 {
		return nil, ErrUnavailable
	}
	reference, err := url.Parse(values[0])
	if err != nil {
		return nil, ErrUnavailable
	}
	location := c.endpoint.ResolveReference(reference)
	if location.Scheme != c.endpoint.Scheme || location.Host != c.endpoint.Host ||
		location.User != nil || location.RawQuery != "" || location.ForceQuery || location.Fragment != "" {
		return nil, ErrUnavailable
	}
	return location, nil
}

func (c *Client) settings() ([]byte, error) {
	var output bytes.Buffer
	output.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	output.WriteString(`<scan:ScanSettings xmlns:scan="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:pwg="http://www.pwg.org/schemas/2010/12/sm">`)
	output.WriteString(`<pwg:Version>2.0</pwg:Version><pwg:ScanRegions pwg:MustHonor="true"><pwg:ScanRegion>`)
	output.WriteString(`<pwg:ContentRegionUnits>escl:ThreeHundredthsOfInches</pwg:ContentRegionUnits>`)
	output.WriteString(`<pwg:XOffset>` + strconv.Itoa(c.options.Region.X) + `</pwg:XOffset>`)
	output.WriteString(`<pwg:YOffset>` + strconv.Itoa(c.options.Region.Y) + `</pwg:YOffset>`)
	output.WriteString(`<pwg:Width>` + strconv.Itoa(c.options.Region.Width) + `</pwg:Width>`)
	output.WriteString(`<pwg:Height>` + strconv.Itoa(c.options.Region.Height) + `</pwg:Height>`)
	output.WriteString(`</pwg:ScanRegion></pwg:ScanRegions><pwg:InputSource>Platen</pwg:InputSource>`)
	color := "RGB24"
	if c.options.Color == "gray" {
		color = "Grayscale8"
	}
	output.WriteString(`<scan:ColorMode>` + color + `</scan:ColorMode>`)
	output.WriteString(`<scan:XResolution>` + strconv.Itoa(c.options.Resolution) + `</scan:XResolution>`)
	output.WriteString(`<scan:YResolution>` + strconv.Itoa(c.options.Resolution) + `</scan:YResolution>`)
	output.WriteString(`<pwg:DocumentFormat>image/jpeg</pwg:DocumentFormat>`)
	if c.options.Intent != "" {
		output.WriteString(`<scan:Intent>`)
		if err := xml.EscapeText(&output, []byte(c.options.Intent)); err != nil {
			return nil, ErrInvalid
		}
		output.WriteString(`</scan:Intent>`)
	}
	output.WriteString(`</scan:ScanSettings>`)
	return output.Bytes(), nil
}

func unavailable(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, ErrUnavailable) {
		return ErrUnavailable
	}
	return ErrUnavailable
}

func mediaType(value string) string {
	media, _, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return media
}

func token(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
