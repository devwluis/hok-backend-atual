package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

func runSkillByName(skillName, input string) (output string, success bool, source string, err error) {
	skills, lerr := listSkills()
	if lerr != nil {
		return "", false, "", lerr
	}
	var selected *Skill
	for i := range skills {
		if strings.EqualFold(skills[i].Name, skillName) {
			selected = &skills[i]
			break
		}
	}
	if selected == nil {
		return "", false, "", fmt.Errorf("skill '%s' nao encontrada", skillName)
	}

	action := strings.ReplaceAll(extractBashFromContent(selected.Content), "DESCRICAO_DO_USUARIO", input)
	action = strings.ReplaceAll(action, "ESTADO_ANTERIOR", getSkillLastOutput(selected.Name))
	if action == "" {
		return "", false, "", fmt.Errorf("skill '%s' sem bloco bash executavel", skillName)
	}

	if termuxSkills[selected.Name] {
		id, ch := enqueueDeviceCommand(selected.Name, action)
		select {
		case <-ch:
			result, _ := getDeviceResult(id)
			success = result.Error == ""
			output = result.Output
			if !success {
				output = result.Error
			}
			return output, success, "device", nil
		case <-time.After(30 * time.Second):
			return "timeout — bridge offline", false, "device", nil
		}
	}

	out, cmdErr := exec.Command("bash", "-c", action).CombinedOutput()
	success = cmdErr == nil
	output = string(out)
	if !success && output == "" {
		output = cmdErr.Error()
	}
	return output, success, "vps", nil
}

const (
	skillRetryMax       = 3
	skillRetryBaseDelay = 2 * time.Second
)

func runSkillWithRetry(skillName, input string) (output string, success bool, source string, err error) {
	for attempt := 1; attempt <= skillRetryMax; attempt++ {
		output, success, source, err = runSkillByName(skillName, input)
		if err != nil {
			return
		}
		if success {
			saveSkillOutput(skillName, output)
			if attempt > 1 {
				log.Printf("skill '%s' teve sucesso na tentativa %d/%d", skillName, attempt, skillRetryMax)
			}
			return
		}
		if attempt < skillRetryMax {
			backoff := skillRetryBaseDelay * time.Duration(1<<(attempt-1))
			log.Printf("skill '%s' falhou (tentativa %d/%d) - retry em %v", skillName, attempt, skillRetryMax, backoff)
			time.Sleep(backoff)
		}
	}
	return
}
