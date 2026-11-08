import Foundation
import Darwin

enum RecoveryClock {
    // Includes sleep, and cannot be extended by changing the wall clock.
    static var now: TimeInterval { Double(clock_gettime_nsec_np(CLOCK_MONOTONIC_RAW)) / 1_000_000_000 }
    // A sleeping Mac is not a frozen app. Liveness time excludes system sleep.
    static var heartbeatNow: TimeInterval { Double(clock_gettime_nsec_np(CLOCK_UPTIME_RAW)) / 1_000_000_000 }
    static let heartbeatTimeout: TimeInterval = 3
    static let acknowledgementTimeout: TimeInterval = 2
    static let maximumTimedSession: TimeInterval = 300
}
