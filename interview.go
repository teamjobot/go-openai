package openai

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

/*
	----- Completion Request Settings -----
	Frequency penalty:
	Lowers the chances of a word being selected again the more times that word has already been used.

	MaxTokens (might make Client arg later):
	- 512 can generate about 51 questions but takes over 20 seconds.
	- 128 about 12 ques in 5+sec.
	- 75 seems good for about 5ish questions ~3 sec

	Presence Penalty:
	Presence penalty does not consider how frequently a word has been used, but just if the word exists in the text.
	This helps to make it less repetitive and seem more natural.

	Temperature:
	Controls randomness. Lowering results in less random. Closer to zero the model becomes more deterministic and
	repetitive.

	TopP:
	Controls diversity via nucleus sampling; 0.5 means half of all likelihood-weighted options are considered.

	TopP vs Temperature: use one or the other, set other to 1:
	"Top P provides better control for applications in which GPT-3 is expected to generate text with accuracy and
	correctness, while Temperature works best for those applications in which original, creative or even amusing
	responses are sought."
*/

const (
	InterviewDefaultCap    = 5
	InterviewDefaultEngine = "text-davinci-001"
	InterviewMaxCap        = 50
)

var (
	newLineRe = regexp.MustCompile(`\r?\n`)
)

type InterviewInput struct {
	JobTitle       *string
	JobDescription *string
}

type InterviewOptions struct {
	// Cap for max number of questions to take
	Cap *int `json:"cap"`

	// If true, received answers are randomly shuffled instead of taken in returned order
	Shuffle bool `json:"shuffle"`
}

func (o InterviewOptions) GetCap() int {
	capped := InterviewDefaultCap

	if o.Cap != nil {
		capped = *o.Cap
	}

	if capped > InterviewMaxCap {
		capped = InterviewMaxCap
	}

	return capped
}

type InterviewRequest struct {
	Prompt   string                   `json:"prompt"`
	Settings InterviewRequestSettings `json:"settings"`
}

// InterviewRequestSettings allows granular overrides of most AI settings.
// Originally wasn't exposing any GPT settings to encapsulate and simplify caller use; later did for more control
// but still not exposing some things that could conflict or confuse.
type InterviewRequestSettings struct {
	Engine           string   `json:"engine"`
	FrequencyPenalty float32  `json:"frequencyPenalty"`
	MaxTokens        *int     `json:"maxTokens"`
	PresencePenalty  float32  `json:"presencePenalty"`
	Temperature      *float32 `json:"temperature"`
	TopP             *float32 `json:"topP"`
	User             string   `json:"user"`
}

type InterviewResponse struct {
	Request   InterviewRequest    `json:"request"`
	Duration  time.Duration       `json:"duration"`
	Options   *InterviewOptions   `json:"options"`
	Questions []InterviewQuestion `json:"questions"`
}

type InterviewQuestion struct {
	Index    int    `json:"index"`
	Question string `json:"question"`
}

func (r *InterviewResponse) HasQuestions() bool {
	return r != nil && r.Questions != nil && len(r.Questions) > 0
}

func (r *InterviewResponse) QuestionText() string {
	var sb strings.Builder

	for index, r := range r.Questions {
		sb.WriteString(r.Question)

		if index+1 < len(r.Question) {
			sb.WriteString("\n\n")
		}
	}

	return sb.String()
}

func formatInterviewInput(input string) string {
	output := newLineRe.ReplaceAllString(input, " ")
	output = strings.ReplaceAll(output, "•", "")
	return output
}

func getInterviewPrompt(jobTitle, jobDesc string) string {
	var prompt string

	// TODO: if cap provided, consider "Create a list of %d questions" with cap
	if len(jobTitle) > 0 && len(jobDesc) > 0 {
		prompt = fmt.Sprintf(
			"Create a list of questions for my interview with a %s, %s",
			formatInterviewInput(jobTitle),
			formatInterviewInput(jobDesc))
	} else if len(jobTitle) > 0 {
		prompt = fmt.Sprintf("Create a list of questions for my interview with a %s", formatInterviewInput(jobTitle))
	} else if len(jobDesc) > 0 {
		prompt = fmt.Sprintf(
			"Create a list of questions for my interview with a job description of %s",
			formatInterviewInput(jobDesc))
	}

	return prompt
}

func NewInterviewOptions(cap int) *InterviewOptions {
	return &InterviewOptions{
		Cap: IntPtr(cap),
	}
}

// NewInterviewSettings creates interview questions request with default optimized settings.
func NewInterviewSettings(user string) *InterviewRequestSettings {
	// See Completion Request Settings comments at top of file
	request := &InterviewRequestSettings{
		Engine:           InterviewDefaultEngine,
		FrequencyPenalty: .75,
		MaxTokens:        IntPtr(175),
		PresencePenalty:  .7,
		Temperature:      Float32Ptr(1),
		TopP:             Float32Ptr(0.85),
		User:             user,
	}
	return request
}

// NewInterviewSettingsRand creates interview questions request with more randomized settings to encourage new results.
func NewInterviewSettingsRand(user string) *InterviewRequestSettings {
	// See Completion Request Settings comments at top of file
	// Use TopP most of the time but temp sometimes
	temp := Float32Ptr(1)
	topP := float32PtrRand(0.3, 0.9)

	rTempTop := random(1, 10)
	if rTempTop > 7 {
		temp = float32PtrRand(0.3, 0.9)
		topP = Float32Ptr(1)
	}

	request := &InterviewRequestSettings{
		Engine:           InterviewDefaultEngine,
		FrequencyPenalty: float32Rand(0.2, 0.85),
		MaxTokens:        intPtrRand(175, 275),
		PresencePenalty:  float32Rand(0.1, 0.8),
		Temperature:      temp,
		TopP:             topP,
		User:             user,
	}

	return request
}

func mapInterviewSettings(settings *InterviewRequestSettings, prompt string) CompletionRequest {
	return CompletionRequest{
		Echo:             false,
		FrequencyPenalty: settings.FrequencyPenalty,
		MaxTokens:        *settings.MaxTokens,
		N:                1,
		PresencePenalty:  settings.PresencePenalty,
		Prompt:           prompt,
		Stream:           false,
		Temperature:      *settings.Temperature,
		TopP:             *settings.TopP,
		User:             settings.User,
		Model:            settings.Engine,
	}
}

func (c *Client) InterviewQuestions(
	ctx context.Context,
	input InterviewInput,
	settings *InterviewRequestSettings,
	options *InterviewOptions) (*InterviewResponse, error) {

	start := time.Now()
	jobTitle := trimStr(input.JobTitle)
	jobDesc := trimStr(input.JobDescription)

	if len(jobTitle) == 0 && len(jobDesc) == 0 {
		return nil, errors.New("must specify a job title or description")
	}

	if settings == nil {
		return nil, errors.New("request settings are required")
	}
	if len(settings.Engine) == 0 {
		settings.Engine = InterviewDefaultEngine
	}
	if options == nil {
		options = NewInterviewOptions(InterviewDefaultCap)
	}

	prompt := getInterviewPrompt(jobTitle, jobDesc)
	request := mapInterviewSettings(settings, prompt)
	quesCap := options.GetCap()

	resp, err := c.CreateCompletion(ctx, request)

	if err != nil {
		return nil, err
	}

	// Trying not to expose GPT-3 types to insulate caller but we are repeating some things w/that
	result := &InterviewResponse{
		Options: options,
		Request: InterviewRequest{
			Prompt:   prompt,
			Settings: *settings,
		},
	}

	// Will only be one result max really
	for _, ch := range resp.Choices {
		items := parseInterviewChoice(ch, options.Shuffle)

		if items != nil {
			// result.Questions = append(result.Questions, items...)
			for _, qu := range items {
				if len(result.Questions) == quesCap {
					break
				}

				// Index is mostly for shuffle case to reset
				qu.Index = len(result.Questions) + 1
				result.Questions = append(result.Questions, qu)
			}
		}
	}

	result.Duration = time.Since(start)

	return result, err
}

func stripLeadingNumbers(question string) string {
	// Often question results are numbered 1), 2), etc. or 1. 2. 3. which we want to strip. Below considers input like:
	// "3. What NAS Solutions (enterprise and scale-out) are you familiar with?"
	result := stripLeadingNumber(question, ".")
	result = stripLeadingNumber(result, ")")
	return result
}

func stripLeadingNumber(question, punc string) string {
	ques := question
	pos := strings.Index(ques, fmt.Sprintf("%s", punc))

	// i.e. "1) " through "99) " or "1. ", "3."
	if pos > -1 && pos <= 2 {
		tmp := ques[0:pos]
		_, err := strconv.Atoi(tmp)

		if err == nil {
			ques = ques[pos+1:]
		}
	}

	return strings.TrimSpace(ques)
}

func parseText(question string) string {
	ques := strings.TrimSpace(question)

	if strings.HasPrefix(ques, "-") {
		ques = ques[1:]
	}

	ques = stripLeadingNumbers(ques)
	return strings.TrimSpace(ques)
}

func Shuffle(questions []InterviewQuestion) {
	for len(questions) > 0 {
		n := len(questions)
		randIndex := random(0, int64(n))
		questions[n-1], questions[randIndex] = questions[randIndex], questions[n-1]
		questions = questions[:n-1]
	}
}

func parseInterviewChoice(ch CompletionChoice, shuffle bool) []InterviewQuestion {
	var data []InterviewQuestion

	if len(ch.Text) == 0 {
		return nil
	}

	parts := strings.Split(ch.Text, "\n")

	for _, part := range parts {
		// Last question can be truncated. Might also need to check ch.FinishReason for length later
		if len(part) > 0 && strings.HasSuffix(part, "?") {
			ques := parseText(part)

			data = append(data, InterviewQuestion{
				Index:    len(data) + 1,
				Question: ques,
			})
		}
	}

	if len(data) == 0 {
		return nil
	} else if shuffle {
		Shuffle(data)
	}

	return data
}
