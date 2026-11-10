package lockwatch

import (
	"context"
	"os/exec"
	"strings"
)

const osascript = "/usr/bin/osascript"

// JXA over the ObjC bridge keeps the build cgo free, as sysauth does. a screensaver alone is not a lock; once it
// asks for the password, com.apple.screenIsLocked fires. the run loop is pumped until the daemon is gone, so one
// killed outright leaves no observer behind
const observer = `ObjC.import('Foundation');
ObjC.import('AppKit');
ObjC.bindFunction('getppid', ['int', []]);

function run() {
  var parent = $.getppid();
  var out = $.NSFileHandle.fileHandleWithStandardOutput;
  function say(what) {
    out.writeData($(what + '\n').dataUsingEncoding($.NSUTF8StringEncoding));
  }
  $.NSDistributedNotificationCenter.defaultCenter.addObserverForNameObjectQueueUsingBlock(
    'com.apple.screenIsLocked', null, null, function () { say('lock'); });
  $.NSWorkspace.sharedWorkspace.notificationCenter.addObserverForNameObjectQueueUsingBlock(
    'NSWorkspaceWillSleepNotification', null, null, function () { say('sleep'); });
  while ($.getppid() === parent) {
    $.NSRunLoop.currentRunLoop.runModeBeforeDate($.NSDefaultRunLoopMode, $.NSDate.dateWithTimeIntervalSinceNow(1));
  }
}`

func watchOS(ctx context.Context, onLock func()) {
	keep(ctx, func(ctx context.Context) {
		scan(exec.CommandContext(ctx, osascript, "-l", "JavaScript", "-e", observer), observed, onLock)
	})
}

func observed(line string) bool {
	line = strings.TrimSpace(line)
	return line == "lock" || line == "sleep"
}
