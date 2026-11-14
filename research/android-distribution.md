# Android distribution without Google Play

Research date: 14 Nov 2026. Scope: how Fyr gets onto Android phones without the Play Store, first for the owner alone, later for a few other people.

## In short

Ship v1 as the PWA the spec already plans, served by `fyrd` over its `tsnet` HTTPS origin, and build no APK until a test on a real phone shows that the PWA install, Web Push or the share target fails from a tailnet-only origin. If a native shell becomes necessary, install it with `adb install -r` over wireless debugging while developing, and let Obtainium watch an `/android/` page that `fyrd` serves on the tailnet for everyday updates. Before Google enforces developer verification globally in 2027, register the package name and signing key under the free limited distribution account; until then, and for adb installs at any time, no registration is needed.

## What changes with Android developer verification

- From 30 Sep 2026, certified devices in Brazil, Indonesia, Singapore and Thailand only install apps registered by verified developers. Google will "expand these protections globally for all apps on certified Android devices in 2027". No global date is published yet. [1][2]
- ADB is exempt. Google: "Unregistered apps can be sideloaded with ADB or advanced flow." [2]
- The advanced flow is a one-time setting in Developer options: confirm no one is coaching you, restart, wait one day, confirm with biometrics or PIN. It then stays on for 7 days or indefinitely. Each install of an unregistered app still shows a warning with "Install Anyway". It began rolling out in August 2026. [3][4]
- The limited distribution account is free, needs no government ID, and needs a Google account with 2-Step Verification, a payments profile and a contact email. Users authorize devices through a QR code or link handshake. [5]
- Device cap: the blog posts and the overview table say 20 devices [2][3][1]; the limited distribution guide says 50 [5]. Treat 20 as the safe number.
- Registration ties the package name to the signing key: you prove ownership by "providing the APK signed with your private key". The key itself is not uploaded. [1]
- A full (unlimited) Android Developer Console account costs a one-time 25 USD and needs identity documents. Fyr does not need it unless it goes past the device cap. [1][6]

For Fyr this means: the PWA is not an APK from Fyr's side, so verification does not touch it. A Fyr APK installed by adb is never affected. A Fyr APK installed by Obtainium, F-Droid, a browser download or Taildrop will need either registration (free, limited) or the advanced flow on each phone once enforcement reaches the phone's country.

## Comparison

| Path | First install | Each update | Auto-update | Signing | Cost | Effect of verification (from 2027) |
|---|---|---|---|---|---|---|
| PWA from the `tsnet` origin (no APK) | Open URL in Chrome, tap Install | None: the service worker updates on next load | Yes, by design | None (Google signs the WebAPK) | Free | None on Fyr's side [7] |
| TWA wrapper (Bubblewrap) | APK install by any path below, plus `assetlinks.json` on the origin | Rarely: web changes need no new APK | Web part yes; shell as per install path | Release key; fingerprint in `assetlinks.json` | Free | Same as the install path used |
| `adb install -r` over wireless debugging | Enable Developer options, pair once with a 6-digit code | One command; phone must have wireless debugging on | No | Any key; debug key works | Free | Exempt [2] |
| APK on a tailnet URL, tapped in the browser (or sent with Taildrop) | Allow "install unknown apps" for the browser, tap the file | Download and tap each time | No | Release key | Free | Needs registration or advanced flow |
| Obtainium watching a tailnet URL | Install Obtainium, add the URL, allow it to install apps | None or one tap: Obtainium checks and installs | Yes, silently on Android 12+ when Obtainium did the first install and the app targets a recent API [8] | Release key | Free | Needs registration or advanced flow |
| Self-hosted F-Droid repo (`fdroidserver`) | Install F-Droid client, add repo URL and fingerprint | `fdroid update` on the desktop, then client installs | Yes on Android 12+ for apps the client installed (F-Droid 1.19+) [9] | Release key plus a separate repo index key [10] | Free | Needs registration or advanced flow |
| Firebase App Distribution | Testers accept an email invite, install App Tester | Upload each build to Google; testers tap update | No automatic install (not sourced; see unsourced claims) | Release key | Free | No source found; assume it needs registration |
| Play internal testing | Excluded by the brief | | | | | |

Other names worth knowing: Accrescent (a store with its own review, not for private apps), Droid-ify and Neo Store (alternative F-Droid clients), Shizuku (lets Obtainium install without prompts on older setups).

## Recommended setup

Commands marked **(not run)** were not executed during this research. The only commands run were `nix eval` lookups of nixpkgs versions on 14 Nov 2026: `fdroidserver` 2.4.5-unstable-2026-06-11, `android-tools` 37.0.0p1, `gradle` 8.14.6, and `androidenv.androidPkgs.androidsdk` evaluates.

### Stage 1: v1 ships as a PWA, no APK

1. Serve the PWA from the `fyrd` `tsnet` origin (`https://<node>.<tailnet>.ts.net`). `tsnet` provides the certificate, so the origin is secure and installable. [11]
2. On the phone (Tailscale connected), open the origin in Chrome and tap Install.
3. Check on a real phone, before building anything native:
   - The app appears as an installed app (a WebAPK), not a home-screen shortcut. In Chrome, open `chrome://webapks` and look for the Fyr entry **(not run)**. The minting is done by a Google server [12], and no source confirms it works for an origin reachable only on a tailnet. If it falls back to a shortcut, the share target does not work, because Android only offers the share target for WebAPK installs. [13]
   - Fyr shows in the Android share sheet for an image (acceptance A3).
   - A Web Push from `fyrd` arrives with the phone locked and Fyr closed (acceptance A2). Chrome on Android delivers Web Push through Google's push service, so `fyrd` needs outbound internet access to it and the phone needs Google Play services.
   - Notification action buttons render. Read `Notification.maxActions` on the phone to learn the limit. [14]
   - A WebAuthn passkey with user verification works (the spec's `verified` level). This is the PWA's equivalent of a biometric prompt.
4. If all four pass, stop here for v1. The spec lists a native mobile app as out of scope, and this research gives no reason to change that.

What a native shell would add over the PWA: inline reply inside a notification (Android `RemoteInput`; Web notifications have buttons but no text field), per-category notification channels the user can mute separately, a push channel that does not depend on Google's push service, home-screen widgets and Quick Settings tiles, and a share target that does not depend on WebAPK minting. Biometric prompts and share target alone do not justify a shell, because the PWA covers both when the WebAPK installs.

A TWA is the cheapest shell, but it has its own private-origin risk: Chrome verifies Digital Asset Links when it launches the TWA and falls back to a Custom Tab with browser UI if verification fails [15]. A 2018 Chromium thread reports that verification failed for intranet origins [16]. Test this before choosing TWA over a small native shell.

### Stage 2: if an APK is needed

5. **Create one long-lived release key** and keep it outside the repo **(not run)**:

   ```sh
   mkdir -p ~/.local/share/fyr-android && chmod 700 ~/.local/share/fyr-android
   keytool -genkeypair -v -storetype PKCS12 \
     -keystore ~/.local/share/fyr-android/release.p12 \
     -alias fyr -keyalg RSA -keysize 4096 -validity 36500 -dname "CN=Fyr"
   chmod 600 ~/.local/share/fyr-android/release.p12
   ```

   - Keep the keystore password in the secret store already used on the machine (agenix, sops-nix or `pass`), not in Nix code: anything in a derivation lands world-readable in `/nix/store`.
   - Keep two offline encrypted copies of the `.p12` and its password (for example a USB stick and a password manager attachment).
   - Let Gradle read the path and password from environment variables in its `signingConfigs` block, so the repo holds no secret.
   - `apksigner` signs with v2 and v3 by default and drops v1 when `minSdk` is 24 or higher (from the apksigner docs as remembered, not re-checked). Verify a build with `apksigner verify --verbose --print-certs app-release.apk` **(not run)** and look for v2 and v3 `true`.
   - Rotation: APK Signature Scheme v3 lets a new key take over through a lineage file made with `apksigner rotate`, signed with both the old and the new key [17][18]. It needs the old key, so it is a planned migration, not a recovery.
   - Losing the key: Android only installs an update signed by the same key [19]. Every phone must uninstall Fyr (losing its local app data) and install a build under the new key. After registration, the package name is tied to the lost key, so the likely outcome is a new package name (inference, not confirmed in Google's pages).

6. **Build on NixOS without Android Studio.** Add an `android` dev shell to the Fyr flake (Fyr has no `flake.nix` yet; the spec plans one) **(not run)**:

   ```nix
   pkgs = import nixpkgs {
     inherit system;
     config = { allowUnfree = true; android_sdk.accept_license = true; };
   };
   buildToolsVersion = "36.0.0";
   android = pkgs.androidenv.composeAndroidPackages {
     platformVersions = [ "36" ];
     buildToolsVersions = [ buildToolsVersion ];
     includeEmulator = false;
     includeNDK = false;
   };
   sdk = "${android.androidsdk}/libexec/android-sdk";

   devShells.${system}.android = pkgs.mkShell {
     packages = [ android.androidsdk pkgs.jdk17 pkgs.android-tools ];
     ANDROID_HOME = sdk;
     GRADLE_OPTS = "-Dorg.gradle.project.android.aapt2FromMavenOverride=${sdk}/build-tools/${buildToolsVersion}/aapt2";
   };
   ```

   - The `aapt2` override and `accept_license` come from the nixpkgs Android docs. Keep `buildToolsVersion` in `build.gradle` equal to the one given to `androidenv`, or aapt2 fails with unclear errors. [20]
   - Check that nixpkgs carries platform 36 and build-tools 36.0.0 at the pinned revision; drop to 35 if not.
   - Reproducibility: `flake.lock` pins the SDK, JDK and Gradle. Gradle still downloads the Android Gradle Plugin and libraries from Maven at build time, so the build is pinned by `gradle/libs.versions.toml` and the Gradle wrapper checksum, not hermetic. A fully hermetic `nix build` needs a Gradle dependency lock fed into a fixed-output derivation (nixpkgs `gradle.fetchDeps`); that is extra work with no value for one user, so skip it until other people build Fyr.
   - Set `versionCode` from the clock (minutes since epoch fits under Android's limit for thousands of years) so every ad hoc build is an upgrade, and `versionName` from `git describe --dirty`.

7. **Development loop: adb over wireless debugging.**
   - Phone: Developer options, Wireless debugging, Pair device with pairing code. Pairing and connecting use different ports, and the pairing port expires after a few minutes. [21]
   - Desktop **(not run)**:

     ```sh
     adb pair <phone-ip>:<pairing-port>     # enter the 6-digit code once
     adb connect <phone-ip>:<connect-port>
     ./gradlew installRelease               # or: adb install -r app/build/outputs/apk/release/app-release.apk
     ```

   - From change to running: one command, if the phone is on Wi-Fi with wireless debugging on. The phone's Tailscale IP also works when both devices are in the same tailnet (community guides, not Google docs) [22]. From memory and not re-checked: the toggle needs a Wi-Fi connection, turns off when the phone changes network, and the connect port changes after each toggle, so expect to re-run `adb connect` often.
   - Do not expose the wireless debugging port beyond the tailnet.

8. **Everyday updates: Obtainium watching a page on `fyrd`.**
   - `fyrd` serves `/android/` on its `tsnet` origin: a plain HTML index linking `fyr-<versionName>-<versionCode>.apk`. A build script copies the signed APK there. The APK holds no secret, so this route can skip device-token auth while staying tailnet-only.
   - Phone: install Obtainium, Add App with the `/android/` URL. Obtainium uses its HTML source: it parses the page's links and, by default, uses a hash of the link as a pseudo-version; a "Version Extraction" regex such as `fyr-([0-9.]+)-` gives a real version instead. [23]
   - Turn on background updates globally and for Fyr. Silent install needs Android 12+, Obtainium to have done the first install, and a recent target API. A failed background install gives no notification. [8]
   - From change to running: run the build script, then wait for the next background check or tap Check for updates in Obtainium.
   - A self-hosted F-Droid repo gives the same end result with more parts: a second signing key for the repo index, `fdroid init`, `fdroid update` after each build, and repo metadata [10]. It only pays off if Fyr ever ships several apps.

9. **Before global enforcement in 2027: register.**
   - Create a limited distribution account in the Android Developer Console, register the package name with an APK signed by the release key, and authorize each phone through the QR or link handshake. [5]
   - Alternative per phone: turn on the advanced flow once (one day wait, then "Install Anyway" on each unregistered install). [3]
   - `adb install` keeps working without either.

10. **A few other people.** Share the `fyrd` node to their tailnet (Tailscale node sharing), give them the PWA URL, and if an APK exists, authorize their phone under the limited distribution account and give them the Obtainium URL. This stays free up to the device cap (20 or 50; see above).

## Risks and what to check

| Risk | How to check |
|---|---|
| WebAPK minting fails for a tailnet-only origin, so the PWA installs as a shortcut and the Android share target (A3) disappears | Stage 1 step 3 on a real phone. No source confirms either outcome. |
| TWA Digital Asset Links check fails for a private origin, so a TWA shows browser UI | Build a throwaway TWA with Bubblewrap and open it on the phone. Only a 2018 report exists [16]. |
| Web Push needs Google's push service; a phone without Google Play services gets no push | Test on the actual phone; a native shell with its own connection over the tailnet is the fallback. |
| Global enforcement date and details for 2027 are not published | Re-read developer.android.com/developer-verification before the first non-adb install in 2027. |
| Device cap is 20 or 50 depending on the Google page | Check the Android Developer Console once the account exists. |
| Unknown whether the advanced flow allows silent Obtainium background updates or asks on each update | Test with an unregistered build after turning on the advanced flow. |
| Release key loss forces a reinstall and probably a new package name | Two offline encrypted backups; restore test once a year. |
| Wireless debugging drops on network change, slowing the adb loop | Accept it for development; use Obtainium for daily use. |
| Gradle builds are pinned but not hermetic | Commit the Gradle wrapper and version catalog; add a dependency lock if others build Fyr. |

## Claims without a source

- Wireless debugging needs a Wi-Fi connection, turns off on network change, and changes its connect port on each toggle.
- `apksigner` defaults to v2 and v3 and skips v1 for `minSdk` 24 or higher.
- Losing the key means a new package name after registration (inferred from how registration ties name and key).
- The Android share target needs a WebAPK install: Chrome's docs say the feature is available "with WebAPK" [13]; whether a tailnet-only origin can mint one is unsourced.
- Firebase App Distribution behaviour under developer verification, and whether App Tester can update without a tap.
- F-Droid unattended updates for apps from a custom (non-f-droid.org) repo.
- Obtainium silent background installs for the HTML source specifically (the wiki's conditions are written for all sources).
- Chrome Web Push on Android depends on Google Play services.
- `gradle.fetchDeps` in nixpkgs for a hermetic Android build; Taildrop as an APK transfer path.

## Sources

1. Android developer verification guide: https://developer.android.com/developer-verification/guides
2. Android Developers Blog, 30 Mar 2026 (updated 29 Sep 2026): https://developer.android.com/blog/posts/android-developer-verification-rolling-out-to-all-developers-on-play-console-and-android-developer-console
3. Android Developers Blog, advanced flow, 19 Mar 2026: https://developer.android.com/blog/posts/android-developer-verification-balancing-openness-and-choice-with-safety
4. 9to5Google, advanced flow rollout, 18 Aug 2026: https://9to5google.com/2026/08/18/google-gradually-rolling-out-androids-advanced-sideloading-ahead-of-developer-verification/
5. Limited distribution guide: https://developer.android.com/developer-verification/guides/limited-distribution
6. Android Developer Console help, choose a distribution: https://support.google.com/android-developer-console/answer/16640817
7. Chromium, Android web app architectures (WebAPK and TWA): https://chromium.googlesource.com/chromium/src/+/main/components/webapps/docs/android_architecture.md
8. Obtainium wiki, how apps are tracked (background update conditions): https://hamster45105.github.io/Obtainium-Wiki/app_tracking
9. F-Droid, This Week in F-Droid, 8 Feb 2024 (1.19 unattended updates): https://f-droid.org/2024/02/08/twif.html
10. F-Droid, set up an app repo: https://f-droid.org/en/docs/Setup_an_F-Droid_App_Repo/
11. Tailscale Serve and HTTPS certificates: https://tailscale.com/kb/1312/serve
12. Stack Overflow via mirror, WebAPK creation failures over VPN: https://kaweah.freedombox.rocks/kiwix/stackoverflow.com_en_all_2023-11/questions/66336077/webapk-creation-failures-on-android-over-vpn
13. Chrome, Web Share Target API: https://developer.chrome.com/docs/capabilities/web-apis/web-share-target
14. MDN, Notification.actions and maxActions: https://developer.mozilla.org/docs/Web/API/Notification/actions
15. Chrome, Trusted Web Activity quick start: https://developer.chrome.com/docs/android/trusted-web-activity/quick-start
16. chromium-dev, 2018, Digital Asset Links validation for intranet TWA failing: https://groups.google.com/a/chromium.org/g/chromium-dev/c/Ki3TZIGfmTY
17. Android Open Source Project, APK signature scheme v3.1: https://source.android.com/docs/security/features/apksigning/v3-1
18. Guardsquare, APK signature scheme v3 context: https://www.guardsquare.com/blog/android-apk-signature-scheme-v3-context-and-new-opportunities
19. Privacy Guides, obtaining apps (same-key update check): https://www.privacyguides.org/en/android/obtaining-apps/
20. nixpkgs manual, Android section: https://github.com/NixOS/nixpkgs/blob/master/doc/languages-frameworks/android.section.md
21. Android Debug Bridge docs, wireless debugging: https://developer.android.com/studio/command-line/adb
22. Community guide, ADB over Tailscale: https://www.mintlify.com/KilzaNiko/apk-extractor-adb/guides/remote-connection
23. Obtainium wiki, app sources (HTML source, version extraction): https://imranr98.github.io/Obtainium-Wiki/sources
