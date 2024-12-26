package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/google/generative-ai-go/genai"
)

type GeminiService struct {
	systemPrompt string
}

var (
	geminiService *GeminiService
	geminiOnce    sync.Once
)

func NewGeminiService() *GeminiService {
	geminiOnce.Do(func() {
		geminiService = &GeminiService{
			systemPrompt: `You are a commit message generator that follows these rules:
1. Write in past tense (e.g., "added", "fixed", "updated")
2. Be concise and direct
3. Output the commit message as raw text without any formatting, quotes, or code blocks
4. Follow the format: <type>(<optional scope>): <commit message>
5. Commit message should starts with lowercase letter.
6. Commit message must be a maximum of 72 characters.
7. Exclude anything unnecessary such as translation. Your entire response will be passed directly into git commit.`,
		}
	})

	return geminiService
}

func (g *GeminiService) GetUserPrompt(
	context *string,
	diff string,
	files []string,
	lastCommits []string,
) (string, error) {
	if *context != "" {
		temp := fmt.Sprintf("Use the following context to understand intent: %s", *context)
		context = &temp
	} else {
		*context = ""
	}

	conventionalTypes, err := json.Marshal(map[string]string{
		"docs":     "Documentation only changes",
		"style":    "Changes that do not affect the meaning of the code (white-space, formatting, missing semi-colons, etc)",
		"refactor": "A code change that neither fixes a bug nor adds a feature",
		"perf":     "A code change that improves performance",
		"test":     "Adding missing tests or correcting existing tests",
		"build":    "Changes that affect the build system or external dependencies",
		"ci":       "Changes to our CI configuration files and scripts",
		"chore":    "Other changes that don't modify src or test files",
		"revert":   "Reverts a previous commit",
		"feat":     "A new feature",
		"fix":      "A bug fix",
	})
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	lastCommitContext := ""
	if len(lastCommits) > 0 {
		lastCommitContext = fmt.Sprintf(
			"These are the last %d commit messages, if your proposed commit message is related to them, please make sure the commit message is consistent:\n%s\n\n",
			len(lastCommits),
			strings.Join(lastCommits, "\n"),
		)
	}

	return fmt.Sprintf(
		`Generate a concise git commit message written in past tense for the following code diff with the given specifications below:

The output response must be in format:
<type>(<optional scope>): <commit message>

%s

Choose a type from the type-to-description JSON below that best describes the git diff:
%s

Neighboring files:
%s

%s

Code diff:
%s`,
		*context,
		conventionalTypes,
		strings.Join(files, ", "),
		lastCommitContext,
		diff,
	), nil
}

func (g *GeminiService) AnalyzeChanges(
	geminiClient *genai.Client,
	ctx context.Context,
	diff string,
	userContext *string,
	relatedFiles *map[string]string,
	modelName *string,
	lastCommits []string,
) (string, error) {
	// format relatedFiles to be dir : files
	relatedFilesArray := make([]string, 0, len(*relatedFiles))
	for dir, ls := range *relatedFiles {
		relatedFilesArray = append(relatedFilesArray, fmt.Sprintf("%s/%s", dir, ls))
	}

	model := geminiClient.GenerativeModel(*modelName)
	safetySettings := []*genai.SafetySetting{
		{
			Category:  genai.HarmCategoryHarassment,
			Threshold: genai.HarmBlockNone,
		},
		{
			Category:  genai.HarmCategoryHateSpeech,
			Threshold: genai.HarmBlockNone,
		},
		{
			Category:  genai.HarmCategoryDangerousContent,
			Threshold: genai.HarmBlockNone,
		},
		{
			Category:  genai.HarmCategorySexuallyExplicit,
			Threshold: genai.HarmBlockNone,
		},
	}

	// Apply safety settings to the model
	model.SafetySettings = safetySettings
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(g.systemPrompt)},
	}

	userPrompt, err := g.GetUserPrompt(userContext, diff, relatedFilesArray, lastCommits)
	if err != nil {
		return "", err
	}

	resp, err := model.GenerateContent(
		ctx,
		genai.Text(userPrompt),
	)
	if err != nil {
		fmt.Println("Error:", err)
		return "", nil
	}

	return fmt.Sprintf("%v", resp.Candidates[0].Content.Parts[0]), nil
}
