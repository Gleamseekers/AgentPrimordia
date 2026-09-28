// skills.go — ap skill 子命令（v7.4 起为真实实现）
//
// 持久化：技能库落盘于 .ap-skills/skills.json（skills.JSONFileStore），
// 跨进程复用；list/add/remove 均针对该持久化库操作。
//
// 说明：verify 为**静态校验**（Validator 结构校验 + 安全扫描），
// 不执行技能步骤——步骤执行需要 SkillExecutor（工具运行期）与测试用例。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agentprimordia/internal/agent/skills"
)

const skillsUsage = `Usage: ap skill <subcommand> [arguments]

Subcommands:
  list             列出本地技能库（持久化）
  add <file>       从 JSON 文件导入技能（结构校验 + 安全扫描）
  remove <id>      从技能库移除技能
  verify <id>      静态校验技能（结构 + 安全扫描，不执行步骤）

Examples:
  ap skill list
  ap skill add ./my-skill.json
  ap skill remove skill-abc123
  ap skill verify skill-abc123
`

// skillStoreDir 技能库持久化目录（相对当前工作目录）。
var skillStoreDir = ".ap-skills"

// skillRegistryPath 技能库持久化文件。
func skillRegistryPath() string {
	return filepath.Join(skillStoreDir, "skills.json")
}

// newSkillStore 构造注入 JSON 文件持久化的技能库（生产构造点）。
func newSkillStore() *skills.Store {
	return skills.NewStore(skills.WithPersistence(skills.NewJSONFileStore(skillRegistryPath())))
}

func runSkill(args []string) error {
	if len(args) == 0 {
		fmt.Print(skillsUsage)
		return nil
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "list":
		return runSkillList(subArgs)
	case "add":
		return runSkillAdd(subArgs)
	case "remove":
		return runSkillRemove(subArgs)
	case "verify":
		return runSkillVerify(subArgs)
	case "--help", "-h", "help":
		fmt.Print(skillsUsage)
		return nil
	default:
		return fmt.Errorf("unknown skill subcommand %q, run \"ap skill --help\"", sub)
	}
}

func runSkillList(args []string) error {
	_ = args
	store := newSkillStore()
	if err := store.PersistError(); err != nil {
		return fmt.Errorf("加载技能库失败: %w", err)
	}
	list := store.List()
	if len(list) == 0 {
		infof("技能库为空（用 ap skill add <file> 导入）")
		return nil
	}
	fmt.Printf("技能库（%s）:\n", skillRegistryPath())
	for _, sk := range list {
		status := string(sk.Status)
		if status == "" {
			status = "-"
		}
		fmt.Printf("  %-28s %-12s %s\n", sk.ID, status, sk.Name)
	}
	return nil
}

func runSkillAdd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ap skill add <file>")
	}
	path := args[0]
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取技能文件失败: %w", err)
	}
	codec := skills.NewCodec()
	skill, err := codec.Decode(data)
	if err != nil {
		return fmt.Errorf("解析技能 JSON 失败: %w", err)
	}
	if skill.ID == "" {
		// 缺 ID 时以文件名兜底，便于快速导入
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		skill.ID = base
	}
	if err := skills.NewValidator().Validate(skill); err != nil {
		return fmt.Errorf("技能校验失败: %w", err)
	}
	for _, warn := range skills.NewValidator().SecurityScan(skill) {
		infof("安全告警: %s", warn)
	}

	if err := os.MkdirAll(skillStoreDir, 0o755); err != nil {
		return fmt.Errorf("创建技能库目录失败: %w", err)
	}
	store := newSkillStore()
	store.Save(skill)
	if err := store.PersistError(); err != nil {
		return fmt.Errorf("持久化技能失败: %w", err)
	}
	successf("已导入技能 %s（持久化于 %s）", skill.ID, skillRegistryPath())
	return nil
}

func runSkillRemove(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ap skill remove <skill-id>")
	}
	store := newSkillStore()
	if _, ok := store.Get(args[0]); !ok {
		return fmt.Errorf("技能 %q 不存在", args[0])
	}
	store.Delete(args[0])
	if err := store.PersistError(); err != nil {
		return fmt.Errorf("持久化技能库失败: %w", err)
	}
	successf("已移除技能 %s", args[0])
	return nil
}

func runSkillVerify(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ap skill verify <skill-id>")
	}
	store := newSkillStore()
	skill, ok := store.Get(args[0])
	if !ok {
		return fmt.Errorf("技能 %q 不存在", args[0])
	}
	validator := skills.NewValidator()
	if err := validator.Validate(skill); err != nil {
		return fmt.Errorf("技能 %s 校验未通过: %w", args[0], err)
	}
	warnings := validator.SecurityScan(skill)
	if len(warnings) == 0 {
		successf("技能 %s 静态校验通过（结构合法、无安全告警）", args[0])
		return nil
	}
	infof("技能 %s 结构合法，但存在 %d 条安全告警：", args[0], len(warnings))
	for _, w := range warnings {
		fmt.Printf("  - %s\n", w)
	}
	return nil
}
