package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared/constant"
)

type ToolFunction func(argumentString string) (string, error)

type Config struct {
	AgentDir        string `json:"agentDir"`
	FeatureDir      string `json:"featureDir"`
	ModelDir        string `json:"modelDir"`
	ProjectDir      string `json:"projectDir"`
	PromptDir       string `json:"promptDir"`
	RuleDir         string `json:"ruleDir"`
	SpecDir         string `json:"specDir"`
	SrcDir          string `json:"srcDir"`
	TaskDir         string `json:"taskDir"`
	TestDir         string `json:"testDir"`
	ToolDir         string `json:"toolDir"`
	ArtifactLogPath string `json:"artifactLogPath"`
}

type Parameter struct {
	Description string `json:"description"`
	Default     string `json:"default"`
}

type Agent struct {
	Description      string               `json:"description"`
	Parameters       map[string]Parameter `json:"parameters"`
	SystemPromptFile string               `json:"systemPromptFile"`
	UserPromptFile   string               `json:"userPromptFile"`
	InputPaths       map[string]InputPath `json:"inputPaths"`
	OutputPath       string               `json:"outputPath"`
	OutputBackupPath string               `json:"outputBackupPath"`
	Tools            []string             `json:"tools"`
	RuleFiles        []string             `json:"ruleFiles"`
	OnNoFunctionCall string               `json:"onNoFunctionCall"`
	Model            string               `json:"model"`
}

type AgentContext struct {
	Name        string
	Agent       Agent
	FlagSet     *flag.FlagSet
	Parameters  map[string]*string
	Config      Config
	Messages    []responses.ResponseInputItemUnionParam
	Tools       map[string]Tool
	Rules       responses.ResponseInputMessageContentListParam
	Files       map[string]File
	Tasks       []string
	Model       Model
	ArtifactLog *log.Logger
	GetEnv      func(string) string
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ToolFile struct {
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	Type               string         `json:"type"`
	GoFunction         string         `json:"goFunction"`
	Terminating        bool           `json:"terminating"`
	InternalParameters map[string]any `json:"internalParameters"`
	Parameters         map[string]any `json:"parameters"`
	Connection         string         `json:"connection"`
	Command            string         `json:"command"`
	Args               []string       `json:"args"`
	DoNotUseTheseTools []string       `json:"doNotUseTheseTools"`
	UseOnlyTheseTools  []string       `json:"useOnlyTheseTools"`
}

type Tool struct {
	ToolFile ToolFile
	Config   Config
	Function ToolFunction
}

type InputPath struct {
	Path        string `json:"path"`
	Description string `json:"description"`
	Language    string `json:"Language"`
}

type File struct {
	Input   InputPath
	Content string
}

type Model struct {
	Model   string `json:"model"`
	BaseUrl string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
}

func getEnv(name string) string {
	return os.Getenv(name)
}

func readJSONFile[V any](configFilePath string) (V, error) {
	var result V
	f, err := os.Open(configFilePath)
	if err != nil {
		log.Fatalf("failed to open config file %s: %v", configFilePath, err)
		return result, err
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	if err := decoder.Decode(&result); err != nil {
		log.Fatalf("failed to decode config file %s: %v", configFilePath, err)
		return result, err
	}
	return result, nil
}

func parseTemplate(templateString string, context any) (string, error) {
	t, err := template.New("parseTemplate").Parse(templateString)
	if err != nil {
		return "", err
	}

	var resultBytes bytes.Buffer
	if err := t.Execute(&resultBytes, context); err != nil {
		return "", err
	}

	return resultBytes.String(), nil
}

func parseTemplateFile(templatePath string, context any) (string, error) {
	t, err := template.ParseFiles(templatePath)
	if err != nil {
		return "", err
	}

	var resultBytes bytes.Buffer
	if err := t.Execute(&resultBytes, context); err != nil {
		return "", err
	}

	return resultBytes.String(), nil
}

func toolInternalWriteResultFile(path string, pathBackup string, artifactLog *log.Logger) ToolFunction {
	return func(argumentsString string) (string, error) {
		// create directory if not exists
		artifactLog.Println("mkdir", filepath.Dir(path))
		err := os.MkdirAll(filepath.Dir(path), os.ModePerm)
		if err != nil {
			return "", err
		}

		var arguments map[string]any
		err = json.Unmarshal([]byte(argumentsString), &arguments)
		if err != nil {
			return fmt.Sprintf("ERROR: Unmarshal of tool parameters failed: %v", err), nil
		}

		content := arguments["file_content"].(string)

		// write patch if pathPatch is set
		if pathBackup != "" {
			oldFile, err := os.ReadFile(path)
			if err == nil {
				artifactLog.Println("write", pathBackup)
				err = os.WriteFile(pathBackup, []byte(oldFile), 0644)
				if err != nil {
					return "", err
				}
			}
		}

		artifactLog.Println("write", path)
		err = os.WriteFile(path, []byte(content), 0644)
		return "", err
	}
}

func toolInternalTaskNew(directory string, artifactLog *log.Logger) ToolFunction {
	return func(argumentsString string) (string, error) {
		var arguments map[string]any
		err := json.Unmarshal([]byte(argumentsString), &arguments)
		if err != nil {
			return fmt.Sprintf("ERROR: Unmarshal of tool parameters failed: %v", err), nil
		}

		anyContent, ok := arguments["file_content"]
		if !ok {
			return "ERROR: Could not write file: parameter \"file_content\" missing, try again, please", nil
		}

		tasks := strings.Split("\n \n"+string(anyContent.(string)), "\n# ")[1:]
		fmt.Println("toolInternalTaskNew: number of tasks", len(tasks))

		for i, task := range tasks {
			taskPath := path.Join(directory, fmt.Sprintf("task_%s_%03d.md", time.Now().UTC().Format("2006-01-02_15-04-05"), i))
			fmt.Println("taskPath", taskPath)

			artifactLog.Println("write", taskPath)
			err := os.WriteFile(taskPath, []byte(task), 0644)
			if err != nil {
				return "", err
			}
		}
		return "", nil
	}
}

func toolInternalTaskComplete(agent AgentContext, artifactLog *log.Logger) ToolFunction {
	return func(argumentsString string) (string, error) {
		taskPath, err := agent.NextTaskFilePaths()
		if err != nil {
			return "", fmt.Errorf("toolInternalTaskComplete: NextTaskFilePaths failed: %v", err)
		}
		oldpath := taskPath[0]
		outputPath, err := parseTemplate(agent.Agent.OutputPath, agent)
		if err != nil {
			return "", fmt.Errorf("toolInternalTaskComplete: parseTemplate failed for '%s': %v", agent.Agent.OutputPath, err)
		}
		newpath := path.Join(outputPath, path.Base(taskPath[0]))

		// create directory if not exists
		artifactLog.Println("mkdir", outputPath)
		err = os.MkdirAll(outputPath, os.ModePerm)
		if err != nil {
			return "", fmt.Errorf("toolInternalTaskComplete: MkdirAll failed for '%s': %v", newpath, err)
		}

		artifactLog.Println("move", oldpath, "to", newpath)
		err = os.Rename(oldpath, newpath)
		if err != nil {
			return "", fmt.Errorf("toolInternalTaskComplete: Rename failed: %v", err)
		}
		return "", nil
	}
}

func toolInternalAskQuestion(argumentsString string) (string, error) {
	var arguments map[string]any
	err := json.Unmarshal([]byte(argumentsString), &arguments)
	if err != nil {
		return fmt.Sprintf("ERROR: Unmarshal of tool parameters failed: %v", err), nil
	}

	question := arguments["question"].(string)
	fmt.Println("The assistent wants you to specify something: ")
	fmt.Println(question)
	fmt.Println("")
	fmt.Print("> ")
	var input string
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		input = scanner.Text()
	}
	//	fmt.Scanln(&input)

	return input, nil
}

func toolInternalListDir(parameters map[string]any, agent AgentContext) ToolFunction {
	return func(argumentsString string) (string, error) {
		//		var arguments map[string]any
		//		json.Unmarshal([]byte(argumentsString), &arguments)
		//		basePath := path.Clean(strings.Replace(arguments["path"].(string), "\\", "/", -1))
		baseDir, ok := parameters["baseDir"].(string)
		if !ok {
			return "", fmt.Errorf("toolInternalListDir: baseDir parameter is missing or not a string")
		}
		baseDir, err := parseTemplate(baseDir, agent)
		if err != nil {
			return "", fmt.Errorf("toolInternalListDir: parseTemplate for baseDir '%s' failed: %v", baseDir, err)
		}
		var ignoreList []string
		if ignore, ok := parameters["ignore"].([]any); ok {
			for _, item := range ignore {
				ignoreList = append(ignoreList, fmt.Sprint(item))
			}
		}
		basePath := "."
		root, err := os.OpenRoot(baseDir)
		if err != nil {
			return "", fmt.Errorf("toolInternalListDir: OpenRoot for basedir: '%s' failed: %v", baseDir, err)
		}
		defer root.Close()

		var result strings.Builder
		result.WriteString("Listing of project directory: ")
		result.WriteString(path.Join(baseDir, basePath))
		result.WriteString("\n")
		result.WriteString("| Name | Size | Modification Time | Is Dir |\n")

		dirs := []string{basePath}
		for {
			var dir string
			dir, dirs = dirs[0], dirs[1:]
			if slices.Contains(ignoreList, dir) {
				continue
			}
			entries, err := fs.ReadDir(root.FS(), dir)
			if err != nil {
				return "", fmt.Errorf("toolInternalListDir: ReadDir for path: '%s' failed: %v", dir, err)
			}
			for _, file := range entries {
				info, _ := file.Info()
				if info.IsDir() {
					dirs = append(dirs, path.Join(dir, file.Name()))
				} else {
					result.WriteString(fmt.Sprintf("| %s | %d | %s | %t |\n", path.Join(dir, file.Name()), info.Size(), info.ModTime(), info.IsDir()))
				}

			}
			if len(entries) <= 0 && dir != "." {
				result.WriteString(fmt.Sprintf("| %s |  |  | true |\n", dir))
			}
			if len(dirs) <= 0 {
				break
			}
		}
		return result.String(), nil
	}
}

func toolInternalReadFile(baseDir string) ToolFunction {
	return func(argumentsString string) (string, error) {
		var arguments map[string]any
		err := json.Unmarshal([]byte(argumentsString), &arguments)
		if err != nil {
			return fmt.Sprintf("ERROR: Unmarshal of tool parameters failed: %v", err), nil
		}

		path := path.Clean(strings.Replace(arguments["path"].(string), "\\", "/", -1))
		root, err := os.OpenRoot(baseDir)
		if err != nil {
			return "", fmt.Errorf("toolInternalReadFile: OpenRoot for basedir: '%s' failed: %v", baseDir, err)
		}
		defer root.Close()
		file, err := fs.ReadFile(root.FS(), path)
		if err != nil {
			return fmt.Sprintf("ERROR: ReadFile for path: '%s' failed: %v", path, err), nil
		}
		return string(file), nil
	}
}

func toolInternalWriteFile(baseDir string, artifactLog *log.Logger) ToolFunction {
	return func(argumentsString string) (string, error) {
		var arguments map[string]any
		err := json.Unmarshal([]byte(argumentsString), &arguments)
		if err != nil {
			return fmt.Sprintf("ERROR: Unmarshal of tool parameters failed: %v", err), nil
		}
		anyContent, ok := arguments["file_content"]
		if !ok {
			return "ERROR: Could not write file: parameter `file_content` missing, try again, please!", nil
		}

		root, err := os.OpenRoot(baseDir)
		if err != nil {
			return "", fmt.Errorf("toolInternalWriteFile: OpenRoot for basedir: '%s' failed: %v", baseDir, err)
		}
		defer root.Close()

		pathCleaned := path.Clean(strings.Replace(arguments["path"].(string), "\\", "/", -1))
		dir := path.Dir(pathCleaned)
		artifactLog.Println("mkdir", path.Join(baseDir, dir))
		err = root.MkdirAll(dir, os.ModePerm)
		if err != nil {
			return "", fmt.Errorf("toolInternalWriteFile: MkdirAll for dir: '%s' failed: %v", path.Join(baseDir, dir), err)
		}

		artifactLog.Println("write", path.Join(baseDir, pathCleaned))
		err = root.WriteFile(pathCleaned, []byte(anyContent.(string)), 0644)
		if err != nil {
			return "", fmt.Errorf("toolInternalWriteFile: WriteFile for path: '%s' failed: %v", pathCleaned, err)
		}
		return "", nil
	}
}

func convertTool(tool Tool) responses.ToolUnionParam {
	return responses.ToolUnionParam{
		OfFunction: &responses.FunctionToolParam{
			Name:        tool.ToolFile.Name,
			Description: openai.String(tool.ToolFile.Description),
			Type:        constant.Function("function"),
			Parameters:  tool.ToolFile.Parameters,
		},
	}
}

func check[V any](result V, err error) V {
	if err != nil {
		log.Fatalf("check failed: %v", err)
		panic(err)
	}
	return result
}

func checkE(err error) {
	if err != nil {
		log.Fatalf("check failed: %v", err)
		panic(err)
	}
}

func startMcpServer(tool Tool) (*mcp.ClientSession, error) {
	ctx := context.Background()

	// Create a new client, with no features.
	client := mcp.NewClient(&mcp.Implementation{Name: tool.ToolFile.Name, Version: "v1.0.0"}, nil)

	cmd, err := parseTemplate(tool.ToolFile.Command, tool)
	if err != nil {
		return nil, fmt.Errorf("startMcpServer parseTemplate for '%s' failed: %v", tool.ToolFile.Command, err)
	}
	var args []string
	for _, arg := range tool.ToolFile.Args {
		parsedArg, err := parseTemplate(arg, tool)
		if err != nil {
			return nil, fmt.Errorf("startMcpServer parseTemplate for '%s' failed: %v", arg, err)
		}
		args = append(args, parsedArg)
	}

	if tool.ToolFile.Connection != "stdio" {
		return nil, fmt.Errorf("startMcpServer: connection '%s' not supportet, use 'stdio'", tool.ToolFile.Connection)
	}
	// Connect to a server over stdin/stdout.
	transport := &mcp.CommandTransport{Command: exec.Command(cmd, args...)}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("startMcpServer client.Connect for '%s' failed: %v", tool.ToolFile.Name, err)
	}

	return session, nil
}

func toolInternalCallMcp(session *mcp.ClientSession, name string) ToolFunction {
	return func(argumentsString string) (string, error) {
		var arguments map[string]any
		json.Unmarshal([]byte(argumentsString), &arguments)

		// Call a tool on the server.
		params := &mcp.CallToolParams{
			Name:      name,
			Arguments: arguments,
		}
		res, err := session.CallTool(context.Background(), params)
		if err != nil {
			return "", fmt.Errorf("CallTool '%s' failed: %v", name, err)
		}
		if res.IsError {
			return "", fmt.Errorf("CallTool '%s' IsError: %v", name, res.GetError())
		}
		var result strings.Builder
		for _, c := range res.Content {
			result.WriteString(c.(*mcp.TextContent).Text)
		}
		return result.String(), nil
	}
}

func (agent AgentContext) NextTaskFilePaths() ([]string, error) {
	taskGlob := path.Join(agent.Config.FeatureDir, *agent.Parameters["name"], "task_*")
	taskPaths, err := filepath.Glob(taskGlob)
	if err != nil {
		return []string{}, fmt.Errorf("NextTaskName: Glob of '%s' failed: %v", taskGlob, err)
	}
	for i, task := range taskPaths {
		taskPaths[i] = filepath.ToSlash(task)
	}
	sort.Strings(taskPaths)
	return taskPaths, nil
}

func (agent AgentContext) NextTask() (string, error) {
	taskPaths, err := agent.NextTaskFilePaths()
	if len(taskPaths) <= 0 {
		return "", fmt.Errorf("No more Tasks available!")
	}
	task, err := os.ReadFile(taskPaths[0])
	if err != nil {
		return "", fmt.Errorf("NextTask: ReadFile of '%s' failed", taskPaths[0])
	}
	return string(task), nil
}

func main() {
	//
	// Start config
	//
	globalParameters := map[string]*string{}
	globalParameters["config"] = flag.String("config", ".sdd/config.json", "path of the config file")
	globalParameters["feature"] = flag.String("feature", "", "name of the feature to work on")
	// TODO: replace name by feature
	globalParameters["name"] = globalParameters["feature"]
	flag.Parse()
	agents := flag.Args()
	fmt.Println("Using config:", *globalParameters["config"])
	fmt.Println("Feature:", *globalParameters["feature"])
	config := check(readJSONFile[Config](*globalParameters["config"]))
	artifactLogFile := check(os.OpenFile(config.ArtifactLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666))
	defer artifactLogFile.Close()

	agent := AgentContext{Tools: map[string]Tool{}, Parameters: globalParameters, Files: map[string]File{}, Rules: responses.ResponseInputMessageContentListParam{}, GetEnv: getEnv}
	agent.Name = agents[0]
	agent.Config = config
	agent.ArtifactLog = log.New(artifactLogFile, agent.Name, log.LstdFlags)
	agent.Agent = check(readJSONFile[Agent](path.Join(config.AgentDir, agents[0]+".json")))
	agent.Model = check(readJSONFile[Model](path.Join(config.ModelDir, agent.Agent.Model+".json")))
	agent.Model.APIKey = check(parseTemplate(agent.Model.APIKey, agent))
	agent.FlagSet = flag.NewFlagSet(agents[0], flag.ExitOnError)
	for parameterName, parameterValue := range agent.Agent.Parameters {
		defaultValue := parameterValue.Default
		if globalParameter, ok := globalParameters[parameterName]; ok && *globalParameter != "" {
			defaultValue = *globalParameter
		}
		agent.Parameters[parameterName] = agent.FlagSet.String(parameterName, defaultValue, parameterValue.Description)
	}
	agent.FlagSet.Parse(agents[1:])
	agents = agent.FlagSet.Args()

	for _, v := range agent.Agent.Tools {
		toolFile := check(readJSONFile[ToolFile](path.Join(config.ToolDir, v+".json")))
		tool := Tool{ToolFile: toolFile, Config: agent.Config}
		if toolFile.Type == "internal" {
			if toolFile.GoFunction == "toolInternalWriteFile" {
				tool.Function = toolInternalWriteResultFile(check(parseTemplate(agent.Agent.OutputPath, agent)), check(parseTemplate(agent.Agent.OutputBackupPath, agent)), agent.ArtifactLog)
			} else if toolFile.GoFunction == "toolInternalTaskNew" {
				tool.Function = toolInternalTaskNew(check(parseTemplate(agent.Agent.OutputPath, agent)), agent.ArtifactLog)
			} else if toolFile.GoFunction == "toolInternalAskQuestion" {
				tool.Function = toolInternalAskQuestion
			} else if toolFile.GoFunction == "toolInternalListDir" {
				tool.Function = toolInternalListDir(tool.ToolFile.InternalParameters, agent)
			} else if toolFile.GoFunction == "toolInternalReadFile" {
				tool.Function = toolInternalReadFile(check(parseTemplate(tool.ToolFile.InternalParameters["baseDir"].(string), agent)))
			} else if toolFile.GoFunction == "toolInternalWriteProjectFile" {
				tool.Function = toolInternalWriteFile(check(parseTemplate(tool.ToolFile.InternalParameters["baseDir"].(string), agent)), agent.ArtifactLog)
			} else if toolFile.GoFunction == "toolInternalTaskComplete" {
				tool.Function = toolInternalTaskComplete(agent, agent.ArtifactLog)
			}
			agent.Tools[toolFile.Name] = tool
		} else if toolFile.Type == "mcp" {
			session := check(startMcpServer(tool))
			mcpTools := check(session.ListTools(context.Background(), nil))
			for _, mcpTool := range mcpTools.Tools {
				agent.Tools[toolFile.Name+"_"+mcpTool.Name] = Tool{ToolFile: ToolFile{
					Name:        toolFile.Name + "_" + mcpTool.Name,
					Description: mcpTool.Description,
					Type:        toolFile.Type,
					Parameters:  mcpTool.InputSchema.(map[string]any),
				},
					Config: agent.Config, Function: toolInternalCallMcp(session, mcpTool.Name)}
			}
		}
	}
	for k, v := range agent.Agent.InputPaths {
		v.Path = check(parseTemplate(v.Path, agent))
		v.Language = check(parseTemplate(v.Language, agent))
		agent.Files[k] = File{Input: v, Content: string(check(os.ReadFile(v.Path)))}
	}
	for _, fileName := range agent.Agent.RuleFiles {
		path := path.Join(agent.Config.RuleDir, fileName)
		agent.Rules = append(agent.Rules, responses.ResponseInputContentParamOfInputText(string(check(os.ReadFile(path)))))
	}
	var agentContexts []AgentContext
	agentContexts = append(agentContexts, agent)

	//
	// Start assistent
	//

	client := openai.NewClient(
		option.WithAPIKey(agent.Model.APIKey),
		option.WithBaseURL(agent.Model.BaseUrl),
		option.WithHeader("HTTP-Referer", "github.com/nielsgts/sdd-agent-orchestra"),
		option.WithHeader("X-OpenRouter-Title", "sdd-agent-orchestra"),
		option.WithHeader("X-OpenRouter-Categories", "cli-agent"),
	)

	agent.Messages = []responses.ResponseInputItemUnionParam{}
	if agent.Agent.SystemPromptFile != "" {
		systemPrompt := check(parseTemplateFile(path.Join(config.PromptDir, check(parseTemplate(agent.Agent.SystemPromptFile, agent))), agent))
		agent.Messages = append(agent.Messages, responses.ResponseInputItemUnionParam{
			OfInputMessage: &responses.ResponseInputItemMessageParam{
				Role: string(responses.EasyInputMessageRoleSystem),
				Content: responses.ResponseInputMessageContentListParam{
					responses.ResponseInputContentParamOfInputText(systemPrompt),
				},
			},
		})
	}
	if len(agent.Rules) > 0 {
		agent.Messages = append(agent.Messages, responses.ResponseInputItemUnionParam{
			OfInputMessage: &responses.ResponseInputItemMessageParam{
				Role:    string(responses.EasyInputMessageRoleDeveloper),
				Content: agent.Rules,
			},
		})
	}
	if agent.Agent.UserPromptFile != "" {
		userPrompt := check(parseTemplateFile(path.Join(config.PromptDir, check(parseTemplate(agent.Agent.UserPromptFile, agent))), agent))
		agent.Messages = append(agent.Messages, responses.ResponseInputItemUnionParam{
			OfInputMessage: &responses.ResponseInputItemMessageParam{
				Role: string(responses.EasyInputMessageRoleUser),
				Content: responses.ResponseInputMessageContentListParam{
					responses.ResponseInputContentParamOfInputText(userPrompt),
				},
			},
		})
	}
	files := responses.ResponseInputMessageContentListParam{}
	for _, file := range agent.Files {
		fileMessage := check(parseTemplateFile(path.Join(config.PromptDir, "file-with-description.md"), file))
		files = append(files, responses.ResponseInputContentParamOfInputText(fileMessage))
	}
	if len(files) > 0 {
		agent.Messages = append(agent.Messages, responses.ResponseInputItemUnionParam{
			OfInputMessage: &responses.ResponseInputItemMessageParam{
				Role:    string(responses.EasyInputMessageRoleUser),
				Content: files,
			},
		})
	}

	tools := []responses.ToolUnionParam{}
	for _, v := range agent.Tools {
		tools = append(tools, convertTool(v))
	}
	featureNamePointer, ok := agent.Parameters["name"]
	featureName := ""
	if ok {
		featureName = *featureNamePointer
	}
	dumpPath := path.Join(agent.Config.FeatureDir, featureName, "dumps")
	runTime := time.Now().UTC().Format("2006-01-02_15-04-05_")
	checkE(os.MkdirAll(dumpPath, os.ModePerm))

	for i := range 1000 {
		var httpResponse *http.Response
		requestBody := responses.ResponseNewParams{
			Model: agent.Model.Model,
			Input: responses.ResponseNewParamsInputUnion{OfInputItemList: agent.Messages},
			Tools: tools,
		}

		checkE(os.WriteFile(path.Join(dumpPath, runTime+agent.Name+"_request_body.json"), check(requestBody.MarshalJSON()), 0644))
		resp := check(client.Responses.New(context.Background(), requestBody, option.WithResponseInto(&httpResponse)))

		checkE(os.WriteFile(path.Join(dumpPath, runTime+agent.Name+"_response.http"), check(httputil.DumpResponse(httpResponse, false)), 0644))
		checkE(os.WriteFile(path.Join(dumpPath, runTime+agent.Name+"_request.http"), check(httputil.DumpRequestOut(httpResponse.Request, false)), 0644))
		checkE(os.WriteFile(path.Join(dumpPath, runTime+agent.Name+"_response_body.json"), []byte(resp.RawJSON()), 0644))

		function_call := false
		terminating := false
		for _, item := range resp.Output {
			agent.Messages = append(agent.Messages, convertOutputToInput(item))
			switch item.Type {
			case "function_call":
				fmt.Println("function_call:" + item.Name)
				tool, ok := agent.Tools[item.Name]
				var outputString string
				if !ok {
					fmt.Println("Error: function not found")
				} else {
					outputString = check(tool.Function(item.Arguments.OfString))
					fmt.Println(outputString)
				}

				agent.Messages = append(agent.Messages, responses.ResponseInputItemUnionParam{
					OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
						Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
							OfString: openai.String(outputString),
						},
						CallID: item.CallID,
					},
				})
				if tool.ToolFile.Terminating {
					terminating = true
				}
				function_call = true
			default:
				for _, content := range item.Content {
					fmt.Println(content.Type + ":")
					fmt.Println(content.Text)
				}
			}
		}
		if !function_call || terminating {
			if !function_call {
				if agent.Agent.OnNoFunctionCall != "" {
					userPrompt := check(parseTemplateFile(path.Join(config.PromptDir, check(parseTemplate(agent.Agent.OnNoFunctionCall, agent))), agent))
					agent.Messages = append(agent.Messages, responses.ResponseInputItemUnionParam{
						OfInputMessage: &responses.ResponseInputItemMessageParam{
							Role: string(responses.EasyInputMessageRoleUser),
							Content: responses.ResponseInputMessageContentListParam{
								responses.ResponseInputContentParamOfInputText(userPrompt),
							},
						},
					})
				} else {
					fmt.Println("terminating: no function_call present")
					break
				}
			} else {
				break
			}
		}
		check := 25
		if i%check == (check - 1) {
			fmt.Printf("The Agent performed %d requests. Do you want to continue? [Y/n]\n", i+1)
			fmt.Print("> ")
			var input string
			fmt.Scanln(&input)
			if input == "n" || input == "N" {
				break
			}
		}
	}
}

func convertOutputToInput(output responses.ResponseOutputItemUnion) responses.ResponseInputItemUnionParam {
	switch output.Type {
	case "message":
		param := output.AsMessage().ToParam()
		return responses.ResponseInputItemUnionParam{OfOutputMessage: &param}
	case "file_search_call":
		param := output.AsFileSearchCall().ToParam()
		return responses.ResponseInputItemUnionParam{OfFileSearchCall: &param}
	case "function_call":
		param := output.AsFunctionCall().ToParam()
		return responses.ResponseInputItemUnionParam{OfFunctionCall: &param}
	case "web_search_call":
		param := output.AsWebSearchCall().ToParam()
		return responses.ResponseInputItemUnionParam{OfWebSearchCall: &param}
	case "computer_call":
		param := output.AsComputerCall().ToParam()
		return responses.ResponseInputItemUnionParam{OfComputerCall: &param}
	case "reasoning":
		param := output.AsReasoning().ToParam()
		return responses.ResponseInputItemUnionParam{OfReasoning: &param}
		//	case "tool_search_call":
		//		param := output.AsToolSearchCall().ToParam()
		//		return responses.ResponseInputItemUnionParam{OfToolSearchCall: &param}
		//	case "tool_search_output":
		//		param := output.AsToolSearchOutput().ToParam()
		//		return responses.ResponseInputItemUnionParam{OfToolSearchOutput: &param}
		//	case "compaction":
		//		param := output.AsCompaction().ToParam()
		//		return responses.ResponseInputItemUnionParam{OfCompaction: &param}
		//	case "image_generation_call":
		//		param := output.AsImageGenerationCall().ToParam()
		//		return responses.ResponseInputItemUnionParam{OfImageGenerationCall: &param}
	case "code_interpreter_call":
		param := output.AsCodeInterpreterCall().ToParam()
		return responses.ResponseInputItemUnionParam{OfCodeInterpreterCall: &param}
		//	case "local_shell_call":
		//		param := output.AsLocalShellCall().ToParam()
		//		return responses.ResponseInputItemUnionParam{OfLocalShellCall: &param}
		//	case "shell_call":
		//		param := output.AsShellCall().ToParam()
		//		return responses.ResponseInputItemUnionParam{OfShellCall: &param}
		//	case "shell_call_output":
		//		param := output.AsShellCallOutput().ToParam()
		//		return responses.ResponseInputItemUnionParam{OfShellCallOutput: &param}
		//	case "apply_patch_call":
		//		param := output.AsApplyPatchCall().ToParam()
		//		return responses.ResponseInputItemUnionParam{OfApplyPatchCall: &param}
		//	case "apply_patch_call_output":
		//		param := output.AsApplyPatchCallOutput().ToParam()
		//		return responses.ResponseInputItemUnionParam{OfApplyPatchCallOutput: &param}
		//	case "mcp_call":
		//		param := output.AsMcpCall().ToParam()
		//		return responses.ResponseInputItemUnionParam{OfMcpCall: &param}
		//	case "mcp_list_tools":
		//		param := output.AsMcpListTools().ToParam()
		//		return responses.ResponseInputItemUnionParam{OfMcpListTools: &param}
		//	case "mcp_approval_request":
		//		param := output.AsMcpApprovalRequest().ToParam()
		//		return responses.ResponseInputItemUnionParam{OfMcpApprovalRequest: &param}
	case "custom_tool_call":
		param := output.AsCustomToolCall().ToParam()
		return responses.ResponseInputItemUnionParam{OfCustomToolCall: &param}
	}
	log.Println("convertOutputToInput: can no convert type: ", output.Type)
	return responses.ResponseInputItemUnionParam{}
}
