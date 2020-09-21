# DMs Guild Search - Discord Bot

## Running

* Copy `example-config.yaml` to `config.yaml`
  * Add your Discord token and the channel ID where you want the bot to post.
  * Edit the rest as desired.
* Run `discord_bot_dmsguild_search`
* It will post matching releases for the current day as they are posted.
  * When it is first run, it will post any earlier posts from the same day.

## Building

* go `build`

## To Do

* Fix the bug that allows browse.php link to sneak through sometimes.
* Break out the main function into smaller functions
