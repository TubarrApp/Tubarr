Tubarr is a CLI program that can be used to crawl your favorite sites for new videos, download them, then tag them with sister-program Metarr. It was made by and for the developer, however it does work.

A channel may be added with a command like:

  tubarr channel add \
  --channel-urls 'https://www.tubesite.com/@CoolChannel' \
  --channel-name 'Cool Channel' \
  --video-directory /home/coolguy/tubarr/{{channel_name}} \
  --json-directory /home/coolguy/tubarr/{{channel_name}} \
  --metarr-meta-ops 'title:date-tag:prefix:ymd','fulltitle:date-tag:prefix:ymd','all-credits:set:Cool Channel' \
  --metarr-default-output-dir /home/coolguy/Videos/{{channel_name}}/{{year}} \
  --notify 'https://< YOUR PLEX SERVER IP OR DOMAIN >:32400/library/sections/1/refresh?X-Plex-Token=< YOUR PLEX TOKEN >|Plex'

Please use "tubarr --help" to see other possible flags.

Set up a CRON job to run the command as a script for Radarr/Sonarr-esque functionality for Tube sites. Utilizes yt-dlp and browser cookies of your specification to allow downloading even from sites requiring authentication such as censored.tv.

Example crontab:
0 */2 * * * tubarr

