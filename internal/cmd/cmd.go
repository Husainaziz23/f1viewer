package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/SoMuchForSubtlety/f1viewer/v2/internal/util"
	"github.com/gdamore/tcell/v2"
)

type Store struct {
	Commands     []Command
	MultiCommads []MultiCommand
	logger       util.Logger
	lang         string
	accentColor  tcell.Color
}

type commandAndArgs []string

type Command struct {
	Title      string         `json:"title"`
	Command    commandAndArgs `json:"command"`
	registry   string
	registry32 string
}

type MultiCommand struct {
	Title   string           `json:"title,omitempty"`
	Targets []ChannelMatcher `json:"targets,omitempty"`
}

type ChannelMatcher struct {
	MatchTitle string         `json:"match_title,omitempty"`
	Command    commandAndArgs `json:"command,omitempty"`
	CommandKey string         `json:"command_key,omitempty"`
}

type CommandContext struct {
	CustomOptions Command
	MetaData      MetaData
	URL           func() (string, error)
}

// MetaData contains title metadata
type MetaData struct {
	PerspectiveTitle string
	Event            string
	Category         string
	Title            string
	Session          string
	Date             time.Time
	Year             string
	Country          string
	Series           string
	EpisodeNumber    int64
	OrdinalNumber    int64
	Circuit          string

	Source interface{}
}

func NewStore(customCommands []Command, multiCommands []MultiCommand, lang string, logger util.Logger, accentColor tcell.Color) *Store {
	store := Store{
		logger:       logger,
		lang:         lang,
		accentColor:  accentColor,
		MultiCommads: multiCommands,
	}

	commands := []Command{
		{
			Title:   "Play with MPV",
			Command: []string{"mpv", "$url", "--alang=" + lang, "--quiet", "--title=$title"},
		},
		{
			Title:      "Play with VLC",
			registry:   "SOFTWARE\\WOW6432Node\\VideoLAN\\VLC",
			registry32: "SOFTWARE\\VideoLAN\\VLC",
			Command:    []string{"vlc", "$url", "--meta-title=$title"},
		},
		{
			Title:   "Play with IINA",
			Command: []string{"iina", "--no-stdin", "$url"},
		},
	}

	for _, c := range commands {
		_, err := exec.LookPath(c.Command[0])
		if err == nil {
			store.Commands = append(store.Commands, c)
		} else if c, found := checkRegistry(c); found {
			store.Commands = append(store.Commands, c)
		}
	}

	if runtime.GOOS == "darwin" {
		store.Commands = append(store.Commands, Command{
			Title:   "Play with QuickTime Player",
			Command: []string{"open", "-a", "quicktime player", "$url"},
		})
	}

	if len(store.Commands) == 0 {
		store.logger.Error("No compatible players found, make sure they are in your PATH environmen variable")
	}

	store.Commands = append(store.Commands, customCommands...)

	return &store
}

func (s *Store) GetCommand(multi ChannelMatcher) Command {
	if multi.CommandKey != "" {
		for _, c := range s.Commands {
			if strings.EqualFold(multi.CommandKey, c.Title) {
				return c
			}
		}
	}

	return Command{
		Title:   "matcher for " + multi.MatchTitle,
		Command: multi.Command,
	}
}

func (s *Store) RunCommand(cc CommandContext) error {
	url, err := cc.URL()
	if err != nil {
		return fmt.Errorf("could not get video URL: %w", err)
	}
	// replace variables
	tmpCommand := make([]string, len(cc.CustomOptions.Command))
	copy(tmpCommand, cc.CustomOptions.Command)
	metadataJson, err := json.MarshalIndent(cc.MetaData, "", "\t")
	if err != nil {
		s.logger.Error("failed to convert metadata to JSON:", err)
	}
	for i := range tmpCommand {
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$url", url)
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$json", string(metadataJson))
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$session", cc.MetaData.Session)
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$event", cc.MetaData.Event)
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$perspective", cc.MetaData.PerspectiveTitle)
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$category", cc.MetaData.Category)
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$episodenumber", strconv.FormatInt(cc.MetaData.EpisodeNumber, 10))
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$season", cc.MetaData.Year)
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$title", cc.MetaData.Title)
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$filename", sanitizeFileName(cc.MetaData.Title))
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$series", cc.MetaData.Series)
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$country", cc.MetaData.Country)
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$circuit", cc.MetaData.Circuit)
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$ordinal", strconv.FormatInt(cc.MetaData.OrdinalNumber, 10))
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$time", cc.MetaData.Date.Format(time.RFC3339))
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$date", cc.MetaData.Date.Format("2006-01-02"))
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$year", cc.MetaData.Year)
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$month", cc.MetaData.Date.Format("01"))
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$day", cc.MetaData.Date.Format("02"))
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$hour", cc.MetaData.Date.Format("15"))
		tmpCommand[i] = strings.ReplaceAll(tmpCommand[i], "$minute", cc.MetaData.Date.Format("04"))
	}
	return s.runCmd(exec.Command(tmpCommand[0], tmpCommand[1:]...))
}

func (s *Store) runCmd(cmd *exec.Cmd) error {
	wdir, err := os.Getwd()
	if err != nil {
		// session.logError("unable to get working directory: ", err)
		wdir = "?"
	}
	user, err := user.Current()
	if err == nil {
		if wdir == user.HomeDir {
			wdir = "~"
		} else {
			wdir = filepath.Base(wdir)
		}
	}

	accentColorString := util.ColortoHexString(s.accentColor)
	fmt.Fprintf(s.logger, "[%s::b][[-]%s[%s]]$[-::-] %s\n", accentColorString, wdir, accentColorString, strings.Join(cmd.Args, " "))

	cmd.Stdout = s.logger
	cmd.Stderr = s.logger

	err = cmd.Start()
	if err != nil {
		return err
	}
	return cmd.Process.Release()
}

func sanitizeFileName(s string) string {
	whitespace := regexp.MustCompile(`\s+`)
	var illegal *regexp.Regexp
	if runtime.GOOS == "windows" {
		illegal = regexp.MustCompile(`[<>:"/\\|?*]`)
	} else {
		illegal = regexp.MustCompile(`/`)
	}
	s = illegal.ReplaceAllString(s, " ")
	s = whitespace.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	return s
}
