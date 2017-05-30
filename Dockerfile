FROM    debian:stable-slim

COPY    ./golem-herder /usr/local/bin/
EXPOSE  80
CMD     ["golem-herder", "serve"]
