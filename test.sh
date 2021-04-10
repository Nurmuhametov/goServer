#!/bin/bash
go build -o main socrates/main.go
./main 127.0.0.1:5703 45 aidar &
P1=$!
./main 127.0.0.1:5703 45 nastenko &
P2=$!
./main 127.0.0.1:5703 45 maxim &
P3=$!
./main 127.0.0.1:5703 45 alexei &
P4=$!
./main 127.0.0.1:5703 45 nastya &
P5=$!
./main 127.0.0.1:5703 45 danis &
P6=$!
./main 127.0.0.1:5703 45 zilya &
P7=$!
./main 127.0.0.1:5703 45 bulat &
P8=$!
./main 127.0.0.1:5703 45 rinat &
P9=$!
./main 127.0.0.1:5703 45 fanzil &
P10=$!
wait $P1 $P2 $P3 $P4 $P5 $P6 $P7 $P8 $P9 $P10
