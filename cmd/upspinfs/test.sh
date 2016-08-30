#!/bin/bash
set -x
set -e

# Copyright 2016 The Upspin Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

ROOT=$1
USER=$2
USERROOT=$ROOT/$USER

mkdir $USERROOT/

# Writing and reading files.
cp ./test.sh $USERROOT/test.sh
cmp ./test.sh $USERROOT/test.sh

# Creating subdirectories.
mkdir $USERROOT/dir

# Writing and reading into subdirectories.
cp ./test.sh $USERROOT/dir/test.sh
cmp ./test.sh $USERROOT/dir/test.sh

# Hard links (really copy on write).
ln $USERROOT/test.sh $USERROOT/cow.sh
cmp $USERROOT/test.sh $USERROOT/cow.sh

# Remove the first but the second remains.
rm $USERROOT/test.sh
if test -e $USERROOT/test.sh
then
	echo rm $USERROOT/test.sh failed to remove
	exit 1
fi
cmp ./test.sh $USERROOT/cow.sh

# Sym links.
ln -s cow.sh $USERROOT/sym.sh
cmp $USERROOT/cow.sh $USERROOT/sym.sh

# Remove the target and the symlink no longer works.
rm $USERROOT/cow.sh
if cmp test.sh $USERROOT/sym.sh 2>/dev/null
then
	echo symlink target removed but symlink still works 1>&2
	exit 1
fi

exit 0
