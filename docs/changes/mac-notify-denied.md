# V0.mac-notify-denied: Fyr.app shows macOS System Settings copy and button when notifications are blocked

→ amends V0.mac-notify (decision 0037).

1. **System Settings wording.** When notifications are denied inside `Fyr.app` (`isNativePush()`), Settings → This device → Notifications now says *"Blocked in System Settings. Allow notifications for Fyr in System Settings → Notifications, then reload Fyr."* instead of telling the user to change browser site settings.
2. **Open System Settings button.** `fyrPush` gains `op: "settings"`, which opens `x-apple.systempreferences:com.apple.Notifications-Settings.extension`, wired to an **Open System Settings** button on the denied row in `Fyr.app`.
3. **Real errors preserved.** `Notifier.turnOn` checks `UNNotificationSettings.authorizationStatus` before returning `denied: true`, returning the actual error text when `requestAuthorization` fails for another reason.
