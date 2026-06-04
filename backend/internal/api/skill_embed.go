package api

import (
	_ "embed"
)

//go:embed skill.md
var embeddedSkill string

func init() {
	SkillMD = embeddedSkill
}
