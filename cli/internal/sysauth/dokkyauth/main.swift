import Foundation
import LocalAuthentication

// dokky's Touch ID helper. macOS names the program asking in its prompt, and this one carries an Info.plist
// saying "dokky", where a script run through osascript reads "osascript". It answers on one line, as the
// script did: check prints touchid, password or none; prompt <reason> prints ok, cancel, unavailable:<why>
// or error:<why>. The reason is an argument, never part of anything that runs.

func describe(_ error: Error?) -> String {
    guard let e = error as NSError? else { return "unknown" }
    return "\(e.localizedDescription) (\(e.code))"
}

// -2 the person cancelled, -4 the system did, -9 dokky did; -5 no passcode, -6 and -7 no Touch ID,
// -10 and -11 locked out or not enrolled, -1004 nobody is there to answer, like a locked screen
func classify(_ error: Error?) -> String {
    guard let e = error as NSError? else { return "error:unknown" }
    switch e.code {
    case -2, -4, -9:
        return "cancel"
    case -5, -6, -7, -10, -11, -1004:
        return "unavailable:" + describe(e)
    default:
        return "error:" + describe(e)
    }
}

func say(_ line: String) -> Never {
    print(line)
    exit(0)
}

let args = CommandLine.arguments
let context = LAContext()

switch args.count > 1 ? args[1] : "" {
case "check":
    if context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: nil) { say("touchid") }
    if context.canEvaluatePolicy(.deviceOwnerAuthentication, error: nil) { say("password") }
    say("none")
case "prompt":
    let reason = args.count > 2 && !args[2].isEmpty ? args[2] : "confirm it is you"
    var problem: NSError?
    guard context.canEvaluatePolicy(.deviceOwnerAuthentication, error: &problem) else {
        say("unavailable:" + describe(problem))
    }
    // Touch ID, with the Mac password as the fallback; the reply comes on another queue
    let done = DispatchSemaphore(value: 0)
    var answer = "error:no reply"
    context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: reason) { ok, error in
        answer = ok ? "ok" : classify(error)
        done.signal()
    }
    // the daemon gives up at two minutes; the prompt goes a little before, so its answer still arrives
    if done.wait(timeout: .now() + 115) == .timedOut {
        context.invalidate()
        say("cancel")
    }
    say(answer)
default:
    FileHandle.standardError.write("usage: dokky-auth check | prompt <reason>\n".data(using: .utf8)!)
    exit(2)
}
