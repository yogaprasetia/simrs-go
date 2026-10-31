#!/bin/sh
# Reset server demo: kosongkan DB sik_demo, migrasi + data contoh, isi skenario demo.
set -e
cd /data/workspace/simrs-demo
systemctl --user stop simrs-demo
sudo -n mariadb -e "DROP DATABASE IF EXISTS sik_demo; CREATE DATABASE sik_demo CHARACTER SET utf8mb4; GRANT ALL ON sik_demo.* TO 'simrs'@'localhost';"
rm -rf uploads/*
./simrs -env simrs.env -migrate
systemctl --user start simrs-demo
sleep 2
python3 seed_demo.py
