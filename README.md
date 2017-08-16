# golem-herder

[![Build Status](https://travis-ci.org/Webstrates/golem-herder.svg?branch=develop)](https://travis-ci.org/Webstrates/golem-herder) [![Go Report Card](https://goreportcard.com/badge/github.com/Webstrates/golem-herder)](https://goreportcard.com/report/github.com/Webstrates/golem-herder) [![Codacy Badge](https://api.codacy.com/project/badge/Grade/db0b35476fa84181b81d2fa7d06ae27f)](https://www.codacy.com/app/Webstrates/golem-herder?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=Webstrates/golem-herder&amp;utm_campaign=Badge_Grade)

The golem-herder is a process which manages golems, minions and daemons. A common theme here is that all of them are external processes which are slaved to a webstrate in some manner.

## Concepts

### Golem

A **golem** is process which connects to a specific webstrate. One golem pr webstrate. It's specifically a docker container running a chrome-headless showing a webstrate. A golem can interact with the webstrate as any normal (browser) client on a webstrate. It therefore forms the glue which allows other procesesses to inspect and manipulate a webstrate. To attach a golem on webstrate two steps are requied.

  1. First the golem-herder must be told to spawn a golem for the webstrate. This is done by issuing a http request to `http(s)://<location-of-herder>/golem/v1/spawn/<id-of-webstrate>`. Normally you'd not do this manually but rather have a script on the webstrate itself handle initialization. Simply include the script at `http(s)://<location-of-herder>/` in the webstrate. `<location-of-herder>` is `emet.cc.au` unless you're running your own golem herder.

  2. The golem will bootstrap itself with code found in the dom element with the query-selector `.golem,#emet`. This code will be run in the headless-chrome. You'd probably want to set up a few websocket connections to the herder to listen for connecting ad-hoc minions, to spawn new controlled minions or daemons.

### Minion

A **minion** is a (light-weight) process which augments the more heavy-weight golems with functionality. A golem can interact with two different types of minions. 
The first is an **ad-hoc** minion which is a process that is not controlled by the herder but rather runs your own hardware. In order to connect a process on your own machine to a golem in a webstrate you should just connect a websocket in your ad-hoc minion to `http(s)://<herder-location>/minion/v1/connect/<webstrate-id>?type=<minion-type>`. The `minion-type` is provided to the golem to let you determine its behaviour towards different types of ad-hoc minions. Once the minion and the golem are connected you can implement a comm protocol to fit your objective.

The second is a **controlled** minion spawned and controlled by the golem. This type of minion is expected to be shortlived (max 30 seconds) and is essentially a mechanism by which an external environment may be used to execute code. It can be a piece of python or ruby code that calculates a result which is returned and used by the golem (and webstrate). Another example is a [minion which converts latex to pdf](https://github.com/Webstrates/minion-latex) to be shown in the browser.

A controlled minion is usually spawned by the golem by letting it send an http POST request to the herder on `http(s)://<herder-location>/minion/v1/spawn`. The request should contain a number of form variables:

 * A form variable with the `env` name determines the environment in which the minion is run. This is translated to a docker image. Current options are `ruby`, `python`, `latex` and `node`. This variable must be set.
 * A form variable with the `output` name determines the file which should be returned as output from the minion. If omitted then a JSON object with `stdout` and `stderr` is returned.
 * Any other form variables are treated as files to be written to the container prior to executing it. E.g. the form variable with `main.sh` and value `echo 'hello'` will get written to the "main.sh" file with "echo 'hello'" as content. The `main.sh` file is normally the file that is executed when the container is started but this is determined by the container image (as selected by the `env` variable).

