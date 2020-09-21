package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/anaskhan96/soup"
	"github.com/bwmarrin/discordgo"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/jasonlvhit/gocron"
)

// Config is the type definition for the YAML configuration
// If you set env-default to something then the cleanenv library doesn't let you
// set the value to empty string in the config :(
type Config struct {
	Discord struct {
		Token   string `yaml:"token" env:"DISCORD_TOKEN"`
		Channel string `yaml:"channel" env:"DISCORD_CHANNEL_ID"`
	} `yaml:"discord"`
	Dmsguild struct {
		Affiliate   string `yaml:"affiliate" env:"DMG_AFFILIATE_ID" env-default:"563484"`
		Keywords    string `yaml:"keywords" env:"DMG_SEARCH_KEYWORDS" env-default:"fantasy%20grounds"`
		TitleFilter string `yaml:"title_filter" env:"DMG_TITLE_FILTER"`
	} `yaml:"dmsguild"`
	Settings struct {
		Minutes string `yaml:"minutes" env:"CHECK_MINUTES" env-default:"15"`
	} `yaml:"settings"`
}

// Args command-line parameters
type Args struct {
	ConfigPath string
}

// global variables
var cfg Config
var lastTitle string
var memoryDate string
var memoryTitles []string

// init Initializes a few paramaters and sets up signal handling
func init() {
	currentTime := time.Now()
	memoryDate = currentTime.Format("2006-01-02")
	memoryTitles = make([]string, 0)

	// Setup our Signal handler
	SetupSignalHandler()
}

// SetupSignalHandler creates a 'listener' on a new goroutine which will notify the
// program if it receives an interrupt from the OS. We then handle this by calling
// our clean up procedure and exiting the program.
func SetupSignalHandler() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		<-c
		fmt.Println("\n[WARN] Received signal. Exiting...")
		os.Exit(99)
	}()
}

// ProcessArgs processes and handles CLI arguments
func ProcessArgs(cfg interface{}) Args {
	var a Args

	f := flag.NewFlagSet("Discord Bot", 1)
	f.StringVar(&a.ConfigPath, "c", "config.yaml", "Path to configuration file")

	fu := f.Usage
	f.Usage = func() {
		fu()
		envHelp, _ := cleanenv.GetDescription(cfg, nil)
		fmt.Fprintln(f.Output())
		fmt.Fprintln(f.Output(), envHelp)
	}

	err := f.Parse(os.Args[1:])
	if err != nil {
		fmt.Println("[ERROR] could not parse CLI arguments: ", err)
		os.Exit(2)
	}
	return a
}

// removeClick removes the [click here for more...] text, if it exists
func removeClick(s string) string {
	fields := strings.Fields(strings.TrimSpace(s))
	workLine := ""
	for _, v := range fields {
		if v == "[click" {
			break
		} else {
			workLine = workLine + v + " "
		}
	}
	final := disableURL(workLine)
	return strings.TrimSpace(final)
}

// disableURL removes http:// and https:// from the descriptions
// to disable additional URL unfurling in Discord
func disableURL(s string) string {
	workLine := strings.Replace(s, "https://", "", -1)
	final := strings.Replace(workLine, "http://", "", -1)
	return strings.TrimSpace(final)
}

// updateMessage is the workhorse and should be split into many smaller functions
// This does all the work of pulling the search results, parsing and then posting them.
func updateMessage(discord *discordgo.Session) error {
	resp, err := soup.Get("https://www.dmsguild.com/browse.php?keywords=" + cfg.Dmsguild.Keywords + "&page=1&sort=4a")
	if err != nil {
		fmt.Println("[ERROR] could perform DMs Guild search: ", err)
		return err
	}
	doc := soup.HTMLParse(resp)
	rows := doc.Find("table", "class", "productListing").FindAll("tr")
	for _, row := range rows {
		message := ""
		link := ""
		price := ""
		//fmt.Printf("%#v", row)
		desc := row.FullText()
		links := row.FindAll("a")
		link = links[0].Attrs()["href"]

		// Split the string into lines.
		parts := strings.Split(desc, "\n")
		// Iterate over the lines.
		firstLine := false
		for _, s := range parts {
			if strings.TrimSpace(s) == "" {
				continue
			} else if !firstLine {
				firstLine = true
				d := regexp.MustCompile(` *Date Added: .*$*`)
				title := d.Split(strings.TrimSpace(s), -1)
				// Filter the titles.
				// Useful for "Fantasy Grounds" amoung others.
				if (cfg.Dmsguild.TitleFilter == "") || (cfg.Dmsguild.TitleFilter != "" && strings.Contains(title[0], cfg.Dmsguild.TitleFilter)) {
					date := strings.Fields(strings.TrimSpace(s))
					finalDate := ""
					endText := ""
					foundDate := "false"
					sendMessage := true
					// DMs Guild HTML code is inconsistent at best.
					// Try to pull out what we want and clean it up a bit.
					for i, v := range date {
						if foundDate == "true" {
							foundDate = "done"
							continue
						}
						if foundDate == "false" {
							if v == "Added:" {
								workDate := date[i+1]
								finalDate = workDate[0:10]
								// Only print today's releases
								currentTime := time.Now()
								if memoryDate != currentTime.Format("2006-01-02") {
									//if memoryDate != "2020-09-17" {
									memoryTitles = make([]string, 0)
									//memoryDate = "2020-09-17"
									memoryDate = currentTime.Format("2006-01-02")
								}
								if finalDate != memoryDate {
									sendMessage = false
								}
								if len(workDate) > 10 {
									endText = workDate[10:] + " "
								}
								foundDate = "true"
							}
						} else {
							endText = endText + v + " "
						}
					}
					foundTitle := false
					for _, v := range memoryTitles {
						if v == title[0] {
							foundTitle = true
							sendMessage = false
							break
						}
					}
					if !foundTitle {
						memoryTitles = append(memoryTitles, title[0])
					}
					if !sendMessage {
						break
					}
					if title[0] == lastTitle {
						break
					}
					message = "**__" + title[0] + "__**\n"
					message = message + "**Date Added**: " + finalDate + "\n"
					message = message + "**Description**:\n"
					if endText != "" {
						message = message + removeClick(endText) + "\n"
					}
				} else {
					break
				}
			} else {
				line := strings.TrimSpace(s)
				if strings.HasPrefix(line, "$") || line == "FREE" {
					price = line
					if strings.Contains(price, " $") {
						price = price + " (**ON SALE**)"
					}
					continue
				}
				if line != "Dungeon Masters Guild" {
					message = message + removeClick(s) + "\n"
				}
			}
		}
		if message == "" {
			continue
		}
		message = message + "[*click the link below for more information*]\n"
		message = message + "**Price**: " + price + "\n"

		message = message + "**Link**: " + link + "?affiliate_id=" + cfg.Dmsguild.Affiliate
		//fmt.Println(message)
		// FIXME: We should not need to check this, but there is a bug that is allowing this to slip through sometimes.
		if !strings.Contains(link, "browse.php") {
			_, err = discord.ChannelMessageSend(cfg.Discord.Channel, message)
			if err != nil {
				fmt.Println("[ERROR] could not send Discord message: ", err)
				return err
			}
		}
	}
	return nil
}

// main is where everything starts.
// Read the config, setup the discord client, run the intial check,
// and then finally setup the ongoinging scheduled checks.
func main() {
	args := ProcessArgs(&cfg)

	// read configuration from the file and environment variables
	if err := cleanenv.ReadConfig(args.ConfigPath, &cfg); err != nil {
		fmt.Println("[ERROR] Reading configuration: ", err)
		os.Exit(2)
	}

	discord, err := discordgo.New("Bot " + cfg.Discord.Token)
	if err != nil {
		fmt.Println("[ERROR] could not create Discord session: ", err)
		os.Exit(1)
	}

	min, err := strconv.ParseInt(cfg.Settings.Minutes, 10, 64)
	if err != nil {
		fmt.Println("[ERROR] could not convert minute argument to integer: ", err)
		os.Exit(1)
	}

	//Run the first time, before the time starts
	err = updateMessage(discord)
	if err != nil {
		fmt.Println("[ERROR] could not perform initial check: ", err)
		os.Exit(1)
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)

	err = gocron.Every(uint64(min)).Minute().Do(updateMessage, discord)
	if err != nil {
		fmt.Println("[ERROR] could not schedule search: ", err)
		os.Exit(1)
	}
	<-gocron.Start()
	wg.Wait()
}
