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
var discord *discordgo.Session
var cfg Config
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
		fmt.Println("\n[" + time.Now().String() + "] [WARN] Received signal. Exiting...")
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
		fmt.Println("["+time.Now().String()+"] [ERROR] could not parse CLI arguments: ", err)
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

// priceClean is used to fixup the price entry a bit
func priceClean(s string) string {
	price := ""
	fields := strings.Fields(strings.TrimSpace(s))
	for i, v := range fields {
		if i == 0 {
			price = "Normal Price: " + v
		} else if i == 1 {
			price = price + "\n**Sales  Price**: " + v
		} else {
			fmt.Println("[" + time.Now().String() + "] [WARN] Found more product price lines then expected.")
		}
	}
	return strings.TrimSpace(price)
}

// searchRows does the initial search and returns the rows we care about
func searchRows() ([]soup.Root, error) {
	resp, err := soup.Get("https://www.dmsguild.com/browse.php?keywords=" + cfg.Dmsguild.Keywords + "&page=1&sort=4a")
	if err != nil {
		fmt.Println("["+time.Now().String()+"] [ERROR] could perform DMs Guild search: ", err)
		return nil, err
	}
	doc := soup.HTMLParse(resp)
	return doc.Find("table", "class", "productListing").FindAll("tr"), nil
}

// handleTitleLine tries to untagle the title and release date
// and then sets up the message template
// FIXME: Could still use some refactoring.
func handleTitleLine(data map[string]string, s string) (map[string]string, error) {
	data["firstLine"] = "true"
	d := regexp.MustCompile(` *Date Added: .*$*`)
	title := d.Split(strings.TrimSpace(s), -1)
	// Filter the titles.
	// Useful for "Fantasy Grounds" amoung others.
	if (cfg.Dmsguild.TitleFilter == "") || (cfg.Dmsguild.TitleFilter != "" && strings.Contains(title[0], cfg.Dmsguild.TitleFilter)) {
		date := strings.Fields(strings.TrimSpace(s))
		finalDate := ""
		endText := ""
		foundDate := "false"
		data["sendMessage"] = "true"
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
						data["sendMessage"] = "false"
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
				data["sendMessage"] = "false"
				break
			}
		}
		if !foundTitle {
			memoryTitles = append(memoryTitles, title[0])
		}
		if data["sendMessage"] == "false" {
			return data, nil
		}
		data["message"] = "**__" + title[0] + "__**\n"
		data["message"] = data["message"] + "**Date Added**: " + finalDate + "\n"
		data["message"] = data["message"] + "**Description**:\n"
		if endText != "" {
			data["message"] = data["message"] + removeClick(endText) + "\n"
		}
	} else {
		return data, nil
	}
	return data, nil
}

// handlePrice tries to return a cleaned up price line for the message.
func handlePrice(data map[string]string, line string) map[string]string {
	match, err := regexp.Match(`\d+\s+\$`, []byte(line))
	if err != nil {
		fmt.Println("["+time.Now().String()+"] [ERROR] could not match price pattern: ", err)
		match = false
	}
	if match {
		data["price"] = priceClean(line)
	} else {
		data["price"] = "**Price**: " + line
	}
	return data
}

// processLines iterates through all the lines for a product
// to try and build a message from the data.
func processLines(parts []string) (map[string]string, error) {
	var err error
	data := make(map[string]string)
	data["message"] = ""
	data["firstLine"] = "false"
	data["price"] = ""
	for _, s := range parts {
		if strings.TrimSpace(s) == "" {
			continue
		} else if data["firstLine"] == "false" {
			data, err = handleTitleLine(data, s)
			if err != nil {
				return nil, err
			}
			if data["sendMessage"] == "false" {
				break
			}
		} else {
			line := strings.TrimSpace(s)
			if strings.HasPrefix(line, "$") || line == "FREE" || line == "Pay What You Want" {
				data = handlePrice(data, line)
				continue
			}
			if line != "Dungeon Masters Guild" {
				data["message"] = data["message"] + removeClick(s) + "\n"
			}
		}
	}
	return data, nil
}

// processRows takes the rows we care about and start to iterate over them.
// This function manages the message creation and sending.
func processRows(rows []soup.Root) error {
	// process the rows
	for _, row := range rows {

		//Grab full text
		desc := row.FullText()

		// Split the full text into lines.
		parts := strings.Split(desc, "\n")

		// Iterate over the lines.
		data, err := processLines(parts)
		if err != nil {
			return err
		}

		if data["message"] == "" {
			continue
		}

		// Grab link
		links := row.FindAll("a")
		data["link"] = links[0].Attrs()["href"]

		// Assemble & send final message
		err = sendMessage(data)
		if err != nil {
			return err
		}
	}
	return nil
}

// sendMessage finalizes the message and sends it to Discord.
func sendMessage(data map[string]string) error {
	data["message"] = data["message"] + "[*click the link below for more information*]\n"
	data["message"] = data["message"] + data["price"] + "\n"

	data["message"] = data["message"] + "**Link**: " + data["link"] + "?affiliate_id=" + cfg.Dmsguild.Affiliate
	//fmt.Println(data["message"])
	// FIXME: We should not need to check this, but there is a bug that is allowing this to slip through sometimes.
	if !strings.Contains(data["link"], "browse.php") {
		_, err := discord.ChannelMessageSend(cfg.Discord.Channel, data["message"])
		if err != nil {
			fmt.Println("["+time.Now().String()+"] [ERROR] could not send Discord message: ", err)
			return err
		}
	}
	return nil
}

// updateMessage coordinates all the work of pulling in the search results,
// parsing and then posting them.
func updateMessage(discord *discordgo.Session) error {
	rows, err := searchRows()
	if err != nil {
		return err
	}

	err = processRows(rows)
	if err != nil {
		return err
	}

	return nil
}

// main is where everything starts.
// Read the config, setup the discord client, run the intial check,
// and then finally setup the ongoinging scheduled checks.
func main() {
	var err error
	args := ProcessArgs(&cfg)

	// read configuration from the file and environment variables
	if err = cleanenv.ReadConfig(args.ConfigPath, &cfg); err != nil {
		fmt.Println("["+time.Now().String()+"] [ERROR] Reading configuration: ", err)
		os.Exit(2)
	}

	fmt.Printf("\n[" + time.Now().String() + "] Initilizing application...\n\n")
	fmt.Println("Keywords for search   : ", cfg.Dmsguild.Keywords)
	fmt.Println("Title filter (if any) : ", cfg.Dmsguild.TitleFilter)
	fmt.Println("Affiliate code        : ", cfg.Dmsguild.Affiliate)
	fmt.Println("Minutes between checks: ", cfg.Settings.Minutes)
	fmt.Printf("\n")

	discord, err = discordgo.New("Bot " + cfg.Discord.Token)
	if err != nil {
		fmt.Println("["+time.Now().String()+"] [ERROR] could not create Discord session: ", err)
		os.Exit(1)
	}

	min, err := strconv.ParseInt(cfg.Settings.Minutes, 10, 64)
	if err != nil {
		fmt.Println("["+time.Now().String()+"] [ERROR] could not convert minute argument to integer: ", err)
		os.Exit(1)
	}

	//Run the first time, before the time starts
	err = updateMessage(discord)
	if err != nil {
		fmt.Println("["+time.Now().String()+"] [ERROR] could not perform initial check: ", err)
		os.Exit(1)
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)

	err = gocron.Every(uint64(min)).Minute().Do(updateMessage, discord)
	if err != nil {
		fmt.Println("["+time.Now().String()+"] [ERROR] could not schedule search: ", err)
		os.Exit(1)
	}
	<-gocron.Start()
	wg.Wait()
}
