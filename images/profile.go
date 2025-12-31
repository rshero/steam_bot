package images

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"os"
	"steam_bot/utils"

	"github.com/GrandpaEJ/advancegg"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	// Card dimensions (16:9)
	CardWidth  = 1280
	CardHeight = 720

	// Avatar settings (40% larger than original 150 -> 210)
	AvatarSize = 210

	// Stats bar settings (doubled height: 140 -> 280, at bottom)
	StatsBarHeight  = 280
	StatsBarY       = CardHeight - StatsBarHeight
	StatsBarOpacity = 0.75

	// Avatar position (on the stats bar, moved up 40%)
	AvatarX = 50
	AvatarY = StatsBarY - 35 - (int(float64(AvatarSize) * 0.4)) // Moved up 40% of avatar size

	// Font paths (relative to project root)
	FontPathRegular = "./assets/fonts/OpenSans-Regular.ttf"
	FontPathBold    = "./assets/fonts/OpenSans-Bold.ttf"

	// Font sizes (in points)
	FontSizeUsername = 40
	FontSizeStats    = 28
)

// Default background color (Steam's dark blue)
var DefaultBgColor = color.RGBA{R: 27, G: 40, B: 56, A: 255}

// ProfileCardOptions contains all data needed to generate a profile card
type ProfileCardOptions struct {
	BackgroundURL string
	AvatarURL     string
	FrameURL      string // optional, empty if no frame
	Username      string
	Level         int
	GameCount     int
	Status        string
}

// GenerateProfileCard creates a profile card image and returns JPEG bytes
func GenerateProfileCard(opts ProfileCardOptions) ([]byte, error) {
	// Create the drawing context
	dc := advancegg.NewContext(CardWidth, CardHeight)

	// 1. Draw background
	if err := drawBackground(dc, opts.BackgroundURL); err != nil {
		// Use default color if background fails
		dc.SetColor(DefaultBgColor)
		dc.Clear()
	}

	// 2. Draw semi-transparent stats bar
	drawStatsBar(dc)

	// 3. Draw avatar (with frame if available)
	if err := drawAvatar(dc, opts.AvatarURL, opts.FrameURL); err != nil {
		// Continue without avatar if it fails
		fmt.Printf("Failed to draw avatar: %v\n", err)
	}

	// 4. Draw stats text
	if err := drawStats(dc, opts); err != nil {
		fmt.Printf("Failed to draw stats: %v\n", err)
	}

	// 5. Encode to PNG (better quality for text)
	var buf bytes.Buffer
	if err := dc.EncodePNG(&buf); err != nil {
		return nil, fmt.Errorf("encoding PNG: %w", err)
	}

	return buf.Bytes(), nil
}

// drawBackground downloads and draws the background image, scaled to fill
func drawBackground(dc *advancegg.Context, url string) error {
	if url == "" {
		return fmt.Errorf("no background URL")
	}

	imgBytes, err := utils.HttpGetBytes(url)
	if err != nil {
		return err
	}

	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return fmt.Errorf("decoding background: %w", err)
	}

	// Scale to fill the card while maintaining aspect ratio
	imgBounds := img.Bounds()
	imgW, imgH := float64(imgBounds.Dx()), float64(imgBounds.Dy())

	scaleX := float64(CardWidth) / imgW
	scaleY := float64(CardHeight) / imgH
	scale := max(scaleX, scaleY) // Use max to cover the entire card

	// Calculate centered position
	scaledW := imgW * scale
	scaledH := imgH * scale
	offsetX := (float64(CardWidth) - scaledW) / 2
	offsetY := (float64(CardHeight) - scaledH) / 2

	dc.Push()
	dc.Translate(offsetX, offsetY)
	dc.Scale(scale, scale)
	dc.DrawImage(img, 0, 0)
	dc.Pop()

	return nil
}

// drawStatsBar draws a semi-transparent dark bar
func drawStatsBar(dc *advancegg.Context) {
	dc.SetRGBA255(78, 80, 90, 200) // Custom color #4E505A with transparency
	dc.DrawRectangle(0, float64(StatsBarY), CardWidth, StatsBarHeight)
	dc.Fill()
}

// drawAvatar downloads and draws the avatar, optionally with a frame
func drawAvatar(dc *advancegg.Context, avatarURL, frameURL string) error {
	if avatarURL == "" {
		return fmt.Errorf("no avatar URL")
	}

	// Download avatar
	avatarBytes, err := utils.HttpGetBytes(avatarURL)
	if err != nil {
		return fmt.Errorf("downloading avatar: %w", err)
	}

	avatarImg, _, err := image.Decode(bytes.NewReader(avatarBytes))
	if err != nil {
		return fmt.Errorf("decoding avatar: %w", err)
	}

	// Draw avatar scaled to AvatarSize
	avatarBounds := avatarImg.Bounds()
	avatarScale := float64(AvatarSize) / float64(avatarBounds.Dx())

	dc.Push()
	dc.Translate(float64(AvatarX), float64(AvatarY))
	dc.Scale(avatarScale, avatarScale)
	dc.DrawImage(avatarImg, 0, 0)
	dc.Pop()

	// Draw frame if available
	if frameURL != "" {
		frameBytes, err := utils.HttpGetBytes(frameURL)
		if err != nil {
			// Frame download failed, continue without it
			return nil
		}

		frameImg, _, err := image.Decode(bytes.NewReader(frameBytes))
		if err != nil {
			return nil
		}

		// Frame should be slightly larger than avatar to surround it
		frameBounds := frameImg.Bounds()
		frameScale := float64(AvatarSize+30) / float64(frameBounds.Dx())

		dc.Push()
		dc.Translate(float64(AvatarX-15), float64(AvatarY-15))
		dc.Scale(frameScale, frameScale)
		dc.DrawImage(frameImg, 0, 0)
		dc.Pop()
	}

	return nil
}

// drawStats draws the username and stats text on the stats bar
func drawStats(dc *advancegg.Context, opts ProfileCardOptions) error {
	// Get the underlying RGBA image from context
	img := dc.Image().(*image.RGBA)

	// Text X position (to the right of avatar) - add small offset to prevent first letter cutoff
	textX := AvatarX + AvatarSize + 45
	textY := StatsBarY + 80 // Baseline position for text

	// Load and draw username (bold, larger)
	fontBytes, err := os.ReadFile(FontPathBold)
	if err != nil {
		return fmt.Errorf("reading bold font: %w", err)
	}

	boldFont, err := opentype.Parse(fontBytes)
	if err != nil {
		return fmt.Errorf("parsing bold font: %w", err)
	}

	boldFace, err := opentype.NewFace(boldFont, &opentype.FaceOptions{
		Size:    FontSizeUsername,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return fmt.Errorf("creating bold face: %w", err)
	}

	drawer := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.White),
		Face: boldFace,
		Dot:  fixed.P(textX, textY),
	}
	drawer.DrawString(opts.Username)

	// Load and draw stats (regular font)
	fontBytes, err = os.ReadFile(FontPathRegular)
	if err != nil {
		return fmt.Errorf("reading regular font: %w", err)
	}

	regularFont, err := opentype.Parse(fontBytes)
	if err != nil {
		return fmt.Errorf("parsing regular font: %w", err)
	}

	regularFace, err := opentype.NewFace(regularFont, &opentype.FaceOptions{
		Size:    FontSizeStats,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return fmt.Errorf("creating regular face: %w", err)
	}

	statsY := textY + 55
	statsText := fmt.Sprintf("Status: %s  |  Level: %d  |  Games: %d",
		opts.Status, opts.Level, opts.GameCount)

	drawer.Face = regularFace
	drawer.Dot = fixed.P(textX, statsY)
	drawer.Src = image.NewUniform(color.White) // Fully opaque white like username
	drawer.DrawString(statsText)

	return nil
}
