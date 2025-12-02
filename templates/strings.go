package templates

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	brRegex           = regexp.MustCompile(`(?i)<br\s*/?>`)
	liOpenRegex       = regexp.MustCompile(`(?i)<li[^>]*>`)
	ulRegex           = regexp.MustCompile(`(?i)</?ul[^>]*>`)
	liCloseRegex      = regexp.MustCompile(`(?i)</li[^>]*>`)
	pRegex            = regexp.MustCompile(`(?i)</?p[^>]*>`)
	divRegex          = regexp.MustCompile(`(?i)</?div[^>]*>`)
	strongOpenRegex   = regexp.MustCompile(`(?i)<strong[^>]*>`)
	strongCloseRegex  = regexp.MustCompile(`(?i)</strong[^>]*>`)
	spanRegex         = regexp.MustCompile(`(?i)</?span[^>]*>`)
	fontRegex         = regexp.MustCompile(`(?i)</?font[^>]*>`)
	headingRegex      = regexp.MustCompile(`(?i)</?h[1-6][^>]*>`)
	minHeaderRegex    = regexp.MustCompile(`(?i)<b>\s*Minimum:?\s*</b>`)
	recHeaderRegex    = regexp.MustCompile(`(?i)<b>\s*Recommended:?\s*</b>`)
	minTextRegex      = regexp.MustCompile(`(?i)Minimum:`)
	recTextRegex      = regexp.MustCompile(`(?i)Recommended:`)
	multiNewlineRegex = regexp.MustCompile(`\n\s*\n`)
)

func FormatDealMessage(title, normalPrice, salePrice, inrPrice, rating, description, imageURL string, categories, genres []string) string {
	if len(description) > 500 {
		description = description[:500] + "..."
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("🎮 <b>%s</b>\n", title))

	if salePrice != "" {
		msg.WriteString(fmt.Sprintf("💸 <b>Price:</b> <code>$%s (was $%s)</code> / <code>%s</code>\n", salePrice, normalPrice, inrPrice))
	} else {
		msg.WriteString(fmt.Sprintf("💸 <b>Price:</b> <code>$%s</code> / <code>%s</code>\n", normalPrice, inrPrice))
	}

	if rating != "" {
		msg.WriteString(fmt.Sprintf("⭐ <b>Steam Rating:</b> <code>%s</code>\n", rating))
	}

	msg.WriteString(fmt.Sprintf("<a href='%s'>&#xad;</a>\n", imageURL))
	msg.WriteString(fmt.Sprintf("<i>%s</i>", description))

	return msg.String()
}

func FormatMoreDetails(title string, categories, genres []string, metacriticScore int, metacriticURL string, reviewDesc string, pos, neg, total int, mainStory, mainExtra, completionist float32, developers, publishers, platforms []string, releaseDate string) string {
	var msg strings.Builder
	msg.Grow(512)
	msg.WriteString(fmt.Sprintf("🎮 <b>%s - Details</b>\n\n", title))

	// Tags
	if len(categories) > 0 {
		msg.WriteString(fmt.Sprintf("🏷️ <b>Tags:</b> %s\n\n", strings.Join(categories, ", ")))
	}

	// Genres
	if len(genres) > 0 {
		msg.WriteString(fmt.Sprintf("🎯 <b>Genres:</b> %s\n\n", strings.Join(genres, ", ")))
	}

	// Metacritic Score
	if metacriticScore > 0 {
		msg.WriteString(fmt.Sprintf("🎖️ <b>Metacritic:</b> %d/100\n\n", metacriticScore))
	}

	// Reviews
	if reviewDesc != "" {
		msg.WriteString(fmt.Sprintf("📊 <b>Reviews:</b> %s\n", reviewDesc))
		msg.WriteString(fmt.Sprintf("👍 %d | 👎 %d (Total: %d)\n\n", pos, neg, total))
	}

	// How Long To Beat
	if mainStory > 0 || mainExtra > 0 || completionist > 0 {
		msg.WriteString("⏱️ <b>How Long To Beat:</b>\n")
		if mainStory > 0 {
			msg.WriteString(fmt.Sprintf("• Main Story: %.2gh\n", mainStory))
		}
		if mainExtra > 0 {
			msg.WriteString(fmt.Sprintf("• Main + Extras: %.2gh\n", mainExtra))
		}
		if completionist > 0 {
			msg.WriteString(fmt.Sprintf("• Completionist: %.2gh\n", completionist))
		}
		msg.WriteString("\n")
	}

	// Platforms
	if len(platforms) > 0 {
		msg.WriteString(fmt.Sprintf("🖥️ <b>Platforms:</b> %s\n", strings.Join(platforms, ", ")))
	}

	// Developers
	if len(developers) > 0 {
		msg.WriteString(fmt.Sprintf("👨‍💻 <b>Developers:</b> %s\n", strings.Join(developers, ", ")))
	}

	// Publishers
	if len(publishers) > 0 {
		msg.WriteString(fmt.Sprintf("🏢 <b>Publishers:</b> %s\n", strings.Join(publishers, ", ")))
	}

	// Release Date
	if releaseDate != "" {
		msg.WriteString(fmt.Sprintf("📅 <b>Release Date:</b> %s\n", releaseDate))
	}

	return msg.String()
}

func FormatRequirementsMessage(title, minReq, recReq string) string {
	var msg strings.Builder
	msg.Grow(512)
	msg.WriteString(fmt.Sprintf("🎮 <b>%s - Requirements</b>\n\n", title))

	if minReq != "" {
		msg.WriteString("💻 <b>Minimum Requirements:</b>\n")
		msg.WriteString(cleanRequirements(minReq) + "\n\n")
	}

	if recReq != "" {
		msg.WriteString("🚀 <b>Recommended Requirements:</b>\n")
		msg.WriteString(cleanRequirements(recReq) + "\n")
	}

	if minReq == "" && recReq == "" {
		msg.WriteString("No requirements information available.")
	}

	return msg.String()
}

func cleanRequirements(req string) string {
	req = brRegex.ReplaceAllString(req, "\n")
	req = liOpenRegex.ReplaceAllString(req, "\n• ")
	req = ulRegex.ReplaceAllString(req, "")
	req = liCloseRegex.ReplaceAllString(req, "")
	req = pRegex.ReplaceAllString(req, "")
	req = divRegex.ReplaceAllString(req, "")
	req = strongOpenRegex.ReplaceAllString(req, "<b>")
	req = strongCloseRegex.ReplaceAllString(req, "</b>")
	req = spanRegex.ReplaceAllString(req, "")
	req = fontRegex.ReplaceAllString(req, "")
	req = headingRegex.ReplaceAllString(req, "\n")
	req = minHeaderRegex.ReplaceAllString(req, "")
	req = recHeaderRegex.ReplaceAllString(req, "")
	req = minTextRegex.ReplaceAllString(req, "")
	req = recTextRegex.ReplaceAllString(req, "")
	req = multiNewlineRegex.ReplaceAllString(req, "\n")

	return strings.TrimSpace(req)
}
