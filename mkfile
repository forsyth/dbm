SHELL=/bin/rc

TARG=\
	delete\
	fetch\
	keys\
	list\
	store\

TARGDIR=${TARG:%=./cmd/%}

all:V:
	for a in $TARGDIR; do go build $a; done
	go vet $TARGDIR

build:V:
	go build

clean:V:
	go clean ./...

vet:V:
	go vet

fmt:V:
	go fmt

nuke:V: clean
	rm -f $TARG
