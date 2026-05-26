SHELL=/bin/rc

TARG=\
	delete\
	fetch\
	keys\
	list\
	store\

TARGDIR=${TARG:%=./cmd/%}

all:V: vet
	for a in $TARGDIR; do go build $a; done

build:V:
	go build

clean:V:
	go clean ./...

vet:V:
	go vet $TARGDIR

fmt:V:
	go fmt ./...

nuke:V: clean
	rm -f $TARG
