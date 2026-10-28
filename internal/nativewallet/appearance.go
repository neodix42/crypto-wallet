package nativewallet

import (
	"localwallet/internal/webui"
	"strconv"
)

func (a *App) appearancePage() []webui.Node {
	appearance := a.appearance.Normalized()
	theme := a.combo("appearanceTheme", "Theme", []webui.Option{{ID: "light", Text: "Light"}, {ID: "dark", Text: "Dark"}})
	theme.Value = appearance.Theme
	var sizes []webui.Option
	for size := 14; size <= 24; size += 2 {
		sizes = append(sizes, webui.Option{ID: strconv.Itoa(size), Text: strconv.Itoa(size) + " px"})
	}
	font := a.combo("appearanceFontSize", "Font size", sizes)
	font.Value = strconv.Itoa(appearance.FontSize)
	return []webui.Node{heading("Appearance"), theme, font, label("Theme and font size apply to every wallet window. These public preferences are stored on this device; wallet keys and credentials are unaffected.")}
}

func (a *App) changeAppearance(key, value string) {
	next := a.appearance.Normalized()
	if key == "appearanceTheme" {
		next.Theme = value
	} else {
		size, err := strconv.Atoi(value)
		if err != nil || strconv.Itoa(size) != value {
			return
		}
		next.FontSize = size
	}
	if !next.Valid() || next == a.appearance.Normalized() {
		return
	}
	if err := webui.SaveAppearance(next); err != nil {
		a.notice("Appearance settings could not be saved. Check access to the local configuration directory.")
		a.render()
		return
	}
	a.appearance = next
	a.render()
}
