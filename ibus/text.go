package ibus

import (
	"github.com/godbus/dbus"
)

type AttrList struct {
	Name        string
	Attachments map[string]dbus.Variant
	Attr        []dbus.Variant
}

type Text struct {
	Name        string
	Attachments map[string]dbus.Variant
	Text        string
	AttrList    dbus.Variant
}

func NewText(text string) *Text {
	attrList := AttrList{}
	attrList.Name = "IBusAttrList"

	t := Text{}
	t.Name = "IBusText"
	t.Text = text
	t.AttrList = dbus.MakeVariant(attrList)

	return &t
}
