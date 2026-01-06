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
	// Card dimensions from Figma (985×554)
	CardWidth  = 985
	CardHeight = 554

	// Figma coordinate offset (to convert from Figma coords to absolute)
	FigmaOffsetX = 547
	FigmaOffsetY = 284

	// Avatar settings from Figma (184×184)
	AvatarSize = 184
	AvatarX    = 61  // Figma: -486 + 547
	AvatarY    = 277 // Figma: -7 + 284

	// Status indicator (on avatar)
	StatusIndicatorSize = 25
	StatusIndicatorX    = 228 // Figma: -319 + 547
	StatusIndicatorY    = 271 // Figma: -13 + 284

	// Stats bar settings from Figma (985×170 at y=384)
	StatsBarHeight = 170
	StatsBarY      = 384 // Figma: 100 + 284
	StatsBarBlur   = 10  // Optimized blur radius (reduced from 47 for performance)
	StatsBarAlpha  = 220 // 60% opacity = 153/255

	// Font paths (BubblegumSans from Figma)
	FontPath = "./assets/fonts/BubblegumSans-Regular.ttf"

	// Font sizes from Figma
	FontSizeUsername    = 36
	FontSizeStats       = 32
	FontSizeGamesPlayed = 20

	// Text positions from Figma
	UsernameX    = 103 // Figma: -444 + 547
	UsernameY    = 499 // Figma: 195 + 284
	LevelTextX   = 261 // Figma: -286 + 547
	LevelTextY   = 414 // Figma: 130 + 284
	ProgressBarX = 261 // Figma: -286 + 547
	ProgressBarY = 463 // Figma: 189 + 284
	GamesPlayedX = 261 // Figma: -286 + 547
	GamesPlayedY = 480 // Figma: 196 + 284
	HoursValueX  = 680 // Figma: 133 + 547
	HoursValueY  = 414 // Figma: 130 + 284
	HoursLabelX  = 625 // Figma: 64 + 547
	HoursLabelY  = 451 // Figma: 167 + 284
	ValueLabelX  = 898 // Figma: 305 + 547
	ValueLabelY  = 406 // Figma: 122 + 284
	ValueAmountX = 893 // Figma: 300 + 547
	ValueAmountY = 451 // Figma: 167 + 284

	// Progress bar settings
	ProgressBarWidth  = 290 // estimated
	ProgressBarHeight = 7   // from Figma
)

// Default background color (Steam's dark blue)
var DefaultBgColor = color.RGBA{R: 27, G: 40, B: 56, A: 255}

// Font cache to avoid reloading font files
var (
	fontCache = make(map[float64]font.Face)
	fontMutex = make(chan struct{}, 1)
)

// ProfileCardOptions contains all data needed to generate a profile card
type ProfileCardOptions struct {
	BackgroundURL string
	AvatarURL     string
	FrameURL      string // optional, empty if no frame
	Username      string
	Level         int
	CountryCode   string
	GameCount     int     // Total games owned
	GamesPlayed   int     // Games with playtime > 0
	TotalHours    float64 // Total hours across all games
	AccountValue  int     // Estimated account value
	Status        string  // Online status
}

// GenerateProfileCard creates a profile card image and returns JPEG bytes
func GenerateProfileCard(opts ProfileCardOptions) ([]byte, error) {
	// Create the drawing context with new Figma dimensions
	dc := advancegg.NewContext(CardWidth, CardHeight)

	// 1. Draw background
	if err := drawBackground(dc, opts.BackgroundURL); err != nil {
		dc.SetColor(DefaultBgColor)
		dc.Clear()
	}

	// 2. Draw semi-transparent stats bar with optimized blur
	drawStatsBarOptimized(dc)

	// 3. Draw avatar with drop shadow and frame
	if err := drawAvatar(dc, opts.AvatarURL, opts.FrameURL, opts.Status); err != nil {
		// Continue without avatar if it fails
	}

	// 4. Draw username below avatar
	if err := drawUsername(dc, opts.Username); err != nil {
		// Continue without username if it fails
	}

	// 5. Draw stats (Level | Country | Games)
	if err := drawLevelStats(dc, opts); err != nil {
		// Continue without stats if it fails
	}

	// 6. Draw progress bar with fixed rounded corners
	drawProgressBarFixed(dc, opts.GamesPlayed, opts.GameCount)

	// 7. Draw "X out of Y games played" text
	if err := drawGamesPlayedText(dc, opts.GamesPlayed, opts.GameCount); err != nil {
		// Continue without text if it fails
	}

	// 8. Draw hours section
	if err := drawHoursSection(dc, opts.TotalHours); err != nil {
		// Continue without hours if it fails
	}

	// 9. Draw value section
	if err := drawValueSection(dc, opts.AccountValue); err != nil {
		// Continue without value if it fails
	}

	// 10. Encode to PNG for lossless quality
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

// drawStatsBarOptimized draws a semi-transparent blurred stats bar
func drawStatsBarOptimized(dc *advancegg.Context) {
	// Create a separate context for the stats bar (smaller image = faster blur)
	statsDC := advancegg.NewContext(CardWidth, StatsBarHeight)
	statsDC.SetRGBA255(78, 80, 90, StatsBarAlpha)
	statsDC.DrawRectangle(0, 0, CardWidth, StatsBarHeight)
	statsDC.Fill()

	// Apply blur (reduced radius for performance)
	statsImg := statsDC.Image()
	blurredImg := advancegg.Blur(StatsBarBlur)(statsImg)

	// Draw the blurred stats bar
	dc.DrawImage(blurredImg, 0, StatsBarY)
}

// drawAvatar downloads and draws the avatar with drop shadow, frame, and status indicator
func drawAvatar(dc *advancegg.Context, avatarURL, frameURL, status string) error {
	if avatarURL == "" {
		return fmt.Errorf("no avatar URL")
	}

	avatarBytes, err := utils.HttpGetBytes(avatarURL)
	if err != nil {
		return fmt.Errorf("downloading avatar: %w", err)
	}

	avatarImg, _, err := image.Decode(bytes.NewReader(avatarBytes))
	if err != nil {
		return fmt.Errorf("decoding avatar: %w", err)
	}

	avatarBounds := avatarImg.Bounds()
	avatarScale := float64(AvatarSize) / float64(avatarBounds.Dx())

	dc.Push()
	dc.Translate(float64(AvatarX), float64(AvatarY))
	dc.Scale(avatarScale, avatarScale)
	dc.DrawImage(avatarImg, 0, 0)
	dc.Pop()

	if frameURL != "" {
		frameBytes, err := utils.HttpGetBytes(frameURL)
		if err != nil {
			return nil
		}

		frameImg, _, err := image.Decode(bytes.NewReader(frameBytes))
		if err != nil {
			return nil
		}

		frameBounds := frameImg.Bounds()
		frameScale := float64(AvatarSize+30) / float64(frameBounds.Dx())

		dc.Push()
		dc.Translate(float64(AvatarX-15), float64(AvatarY-15))
		dc.Scale(frameScale, frameScale)
		dc.DrawImage(frameImg, 0, 0)
		dc.Pop()
	}

	drawStatusIndicator(dc, status)

	return nil
}

// drawStatusIndicator draws a status indicator circle
func drawStatusIndicator(dc *advancegg.Context, status string) {
	var statusColor color.RGBA
	switch status {
	case "Online":
		statusColor = color.RGBA{R: 87, G: 186, B: 115, A: 255}
	case "Away":
		statusColor = color.RGBA{R: 237, G: 193, B: 75, A: 255}
	case "Busy":
		statusColor = color.RGBA{R: 186, G: 87, B: 87, A: 255}
	default:
		statusColor = color.RGBA{R: 140, G: 140, B: 140, A: 255}
	}

	dc.SetRGBA255(182, 182, 182, 214)
	dc.DrawCircle(float64(StatusIndicatorX+StatusIndicatorSize/2),
		float64(StatusIndicatorY+StatusIndicatorSize/2),
		float64(StatusIndicatorSize)/2+2.5)
	dc.Fill()

	dc.SetColor(statusColor)
	dc.DrawCircle(float64(StatusIndicatorX+StatusIndicatorSize/2),
		float64(StatusIndicatorY+StatusIndicatorSize/2),
		float64(StatusIndicatorSize)/2)
	dc.Fill()
}

// drawUsername draws the username below the avatar
func drawUsername(dc *advancegg.Context, username string) error {
	img := dc.Image().(*image.RGBA)

	face, err := loadFont(FontSizeUsername)
	if err != nil {
		return err
	}

	drawer := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.White),
		Face: face,
		Dot:  fixed.P(UsernameX, UsernameY),
	}
	drawer.DrawString(username)

	return nil
}

// drawLevelStats draws "Level X | Country | Y Games"
func drawLevelStats(dc *advancegg.Context, opts ProfileCardOptions) error {
	img := dc.Image().(*image.RGBA)

	face, err := loadFont(FontSizeStats)
	if err != nil {
		return err
	}

	statsText := fmt.Sprintf("Level %d | %s | %d Games",
		opts.Level, opts.CountryCode, opts.GameCount)

	adjustedY := LevelTextY + 26

	drawer := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.White),
		Face: face,
		Dot:  fixed.P(LevelTextX, adjustedY),
	}
	drawer.DrawString(statsText)

	return nil
}

// drawProgressBarFixed draws the games progress bar with proper rounded corners using clipping
func drawProgressBarFixed(dc *advancegg.Context, gamesPlayed, totalGames int) {
	if totalGames == 0 {
		return
	}

	progress := float64(gamesPlayed) / float64(totalGames)
	filledWidth := float64(ProgressBarWidth) * progress
	radius := float64(ProgressBarHeight) / 2

	// Draw background bar (gray, full width) with rounded ends (capsule shape)
	dc.SetRGBA255(210, 210, 210, 255)
	dc.DrawRoundedRectangle(float64(ProgressBarX), float64(ProgressBarY),
		float64(ProgressBarWidth), float64(ProgressBarHeight), radius)
	dc.Fill()

	// Draw filled portion with proper rounded corners using clipping
	if filledWidth > 0 {
		// Save state and clip to the full progress bar shape
		dc.Push()
		dc.DrawRoundedRectangle(float64(ProgressBarX), float64(ProgressBarY),
			float64(ProgressBarWidth), float64(ProgressBarHeight), radius)
		dc.Clip()

		// Draw the filled portion (it will be clipped to match the background's rounded corners)
		dc.SetRGBA255(129, 129, 129, 255)
		dc.DrawRectangle(float64(ProgressBarX), float64(ProgressBarY), filledWidth, float64(ProgressBarHeight))
		dc.Fill()

		dc.Pop()
	}
}

// drawGamesPlayedText draws "X out of Y games played"
func drawGamesPlayedText(dc *advancegg.Context, gamesPlayed, totalGames int) error {
	img := dc.Image().(*image.RGBA)

	face, err := loadFont(FontSizeGamesPlayed)
	if err != nil {
		return err
	}

	text := fmt.Sprintf("%d out of %d games played", gamesPlayed, totalGames)

	adjustedY := GamesPlayedY + 16

	drawer := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.White),
		Face: face,
		Dot:  fixed.P(GamesPlayedX, adjustedY),
	}
	drawer.DrawString(text)

	return nil
}

// drawHoursSection draws the hours wasted section
func drawHoursSection(dc *advancegg.Context, hours float64) error {
	img := dc.Image().(*image.RGBA)

	valueFace, err := loadFont(FontSizeStats)
	if err != nil {
		return err
	}

	hoursValue := fmt.Sprintf("%.1f", hours)

	adjustedValueY := HoursValueY + 26
	adjustedLabelY := HoursLabelY + 26

	drawer := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.White),
		Face: valueFace,
		Dot:  fixed.P(HoursValueX, adjustedValueY),
	}
	drawer.DrawString(hoursValue)

	drawer.Dot = fixed.P(HoursLabelX, adjustedLabelY)
	drawer.DrawString("Hours wasted")

	return nil
}

// drawValueSection draws the account value section
func drawValueSection(dc *advancegg.Context, value int) error {
	img := dc.Image().(*image.RGBA)

	face, err := loadFont(FontSizeStats)
	if err != nil {
		return err
	}

	adjustedLabelY := ValueLabelY + 26
	adjustedAmountY := ValueAmountY + 26

	drawer := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.White),
		Face: face,
	}

	drawer.Dot = fixed.P(ValueLabelX, adjustedLabelY)
	drawer.DrawString("Value")

	valueText := fmt.Sprintf("\u20B9 %d", value)
	drawer.Dot = fixed.P(ValueAmountX, adjustedAmountY)
	drawer.DrawString(valueText)

	return nil
}

// loadFont loads and creates a font face with the specified size (with caching)
func loadFont(size float64) (font.Face, error) {
	fontMutex <- struct{}{}
	defer func() { <-fontMutex }()

	// Check cache first
	if face, ok := fontCache[size]; ok {
		return face, nil
	}

	fontBytes, err := os.ReadFile(FontPath)
	if err != nil {
		return nil, fmt.Errorf("reading font: %w", err)
	}

	parsedFont, err := opentype.Parse(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing font: %w", err)
	}

	face, err := opentype.NewFace(parsedFont, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("creating font face: %w", err)
	}

	// Cache the face
	fontCache[size] = face

	return face, nil
}
