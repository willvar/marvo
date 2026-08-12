#!/bin/sh
set -eu
wget -q -T 2 -O /dev/null http://127.0.0.1:4097/health
