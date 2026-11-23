// The app shell: state, commands and the keyboard.

import { type Zone } from "./app-util.tsx";
import { render } from "preact";
import { useAppState } from "./app-state.tsx";
import { useNavHistory } from "./app-history.tsx";
import { useConnection } from "./app-connection.tsx";
import { useAnswers, useDecisionCards } from "./app-cards.tsx";
import { useThreadActions } from "./app-actions.tsx";
import { useContextMenu } from "./app-menu.tsx";
import { useThreadList } from "./app-list.tsx";
import { useProjectActions } from "./app-projects.tsx";
import { useKeyboard } from "./app-keyboard.tsx";
import { useHostActions } from "./app-hosts.tsx";
import { usePhoneNav } from "./app-nav.tsx";
import type { Thread } from "./types.ts";
import { createApi } from "./api.ts";
import { ApiContext } from "./api-context.ts";
import {
  sectionOf,
  type SectionKey,
  bannerDeploy,
  profilesOn,
  type View,
} from "./store.ts";
import { setAtEnd } from "./seen.ts";
import { keyHints } from "./keys.ts";
import { SetupInboxCard } from "./setup-ui.tsx";
import { ChangesBoard } from "./board-ui.tsx";
import { needsCount } from "./board.ts";
import { Inbox, Rail } from "./inbox.tsx";
import { UndContext } from "./undeclared-ctx.tsx";
import { ThreadPane } from "./thread.tsx";
import { HandoverDialog } from "./handover-ui.tsx";
import { setToolDetails } from "./prefs.ts";
import { ListResizer } from "./list-resize-ui.tsx";
import { PreviewViewer } from "./preview-ui.tsx";
import { Modal, NewThreadDialog, SnoozeMenu } from "./dialogs.tsx";
import { snoozeKeyCount } from "./snooze.ts";
import { isStandalone, promptInstall } from "./pwa.ts";
import { PhoneApp } from "./phone.tsx";
import { ConfirmProvider } from "./confirm-ui.tsx";
import { DirectoryContext, ProjectPicker, QuietGate } from "./project-ui.tsx";
import { recordOf } from "./projects.ts";
import { FolderPickContext } from "./folder-picker.tsx";
import { HostsContext, ProfilesContext } from "./host-pick-ui.tsx";
import { HostMarksContext } from "./host-mark-ui.tsx";
import { DeployBanner } from "./deploy-ui.tsx";
import { StallBanner } from "./stall-ui.tsx";
import { installPress, Pop } from "./motion-ui.tsx";
import { Boundary } from "./boundary.tsx";
import { HomeScreen } from "./home-ui.tsx";

/** V0.status-home: the keys the status screen names at its foot. */
const HOME_KEYS: [string, string][] = [
  ["j/k", "pick"],
  ["Enter", "open"],
  ["c", "new thread"],
  ["?", "keys"],
];

const hostApi = await createApi(); // #42: the mock Api is a chunk loaded only in mock mode

function App() {
  const A = useAppState();
  const N = useNavHistory({ ...A });
  const B = useConnection({ ...A, ...N });
  const C = useDecisionCards({ ...A, ...N, ...B });
  const C2 = useAnswers({ ...A });
  const D = useThreadActions({ ...A, ...N, ...B, ...C });
  const E = useContextMenu({ ...A, ...N, ...B, ...C, ...C2, ...D });
  const F = useThreadList({ ...A, ...N, ...B, ...C, ...D, ...E });
  const G = useProjectActions({ ...A, ...N, ...B, ...C, ...D, ...E, ...F });
  useKeyboard({ ...A, ...N, ...B, ...C, ...D, ...E, ...F, ...G });
  const I = useHostActions({ ...A, ...N, ...B, ...C, ...D, ...E, ...F, ...G });
  const J = usePhoneNav({ ...A, ...N, ...B, ...C, ...D, ...E, ...F, ...G, ...I });
  const { api, rawStore, fullStore, store, byHost, view, sel, zone, setZone, picked, setPicked, dialog, setDialog, snoozeTargets, handoverId, setTracker, setCards, sectionOpen, query, setQuery, help, searchRef, expanded, setExpanded, theme, inText, toolDetailsOn, phone, cmds, toast, nav, selecting, setSelecting, pushSt, canInstall, setCanInstall, env, pushReason, now, setProjectFilter, settingsAt, setSettingsAt, pickIds, filterId, unlock, filtered, searchable, searchList, quietHits, sec, filterView, gate, gatedThread, setArrival, arrivedHere, rows, guest, viewThread, navGo, navReplace, navBack, earlier, loadEarlier, decision, card, answers, openThread, closePeek, closeThread, settle, settleMerged, archive, openSnooze, snoozeList, snooze, runDeskAction, ctxMenu, submitAsk, cancelAsk, answerField, allow, winW, listOn, toggleList, toggleSection, nameSection, sectionUsed, hideShown, jump, send, projectActions, folderPicker, confirmHost, hostsCtx, folderPickView, openProjectScreen, directory, create, restartNow, cycleTheme, pwaControls, settingsFor, phoneActions, phoneSettingsGo, phoneSettingsReplace, undNav } = { ...A, ...N, ...B, ...C, ...C2, ...D, ...E, ...F, ...G, ...I, ...J };

  if (phone) {
    return (
      <DirectoryContext.Provider value={directory}>
        <FolderPickContext.Provider value={folderPicker}>
        <UndContext.Provider value={undNav}>
<HostsContext.Provider value={hostsCtx}>
<HostMarksContext.Provider value={store.hostMarks}>
        <ConfirmProvider value={confirmHost.ask}>
        <ProfilesContext.Provider value={byHost}>
        <Boundary name="Fyr">
        <PreviewViewer />
        <PhoneApp
          nav={{ at: nav, go: navGo, replace: navReplace, back: navBack }}
          store={store}
          profiles={byHost}
          allProjects={fullStore.projects}
          inbox={{ sec, rows, guest, searchable, all: filtered, unlock }}
          now={now}
          commands={cmds}
          open={{
            thread: nav.screen.name === "thread" && !gate.gated ? store.threads[nav.screen.id] : undefined,
            gated: gatedThread,
            events: nav.screen.name === "thread" ? store.events[nav.screen.id] : undefined,
            onEarlier: view && earlier[view.id] ? () => loadEarlier(view.id) : undefined,
            onAtEnd: (b) => setTracker((tr) => setAtEnd(tr, b)),
            card,
            onCard: (fn) => decision && setCards((c) => (c[decision.id] ? { ...c, [decision.id]: fn(c[decision.id]) } : c)),
            submitAsk: answers.submit,
            cancelAsk: answers.cancel,
            allow: answers.allow,
            arrival: arrivedHere,
            setArrival,
          }}
          list={{ sectionOpen, toggleSection, nameSection, hideAt: hideShown, sectionUsed, expanded, onExpand: (k) => setExpanded((x) => ({ ...x, [k]: true })), picked, setPicked, selecting, setSelecting }}
          device={{
            push: pushSt,
            pushReason,
            ios: env.ios,
            standalone: env.standalone,
            canInstall: canInstall && !isStandalone(),
            onInstall: () => void promptInstall().then((ok) => ok && setCanInstall(false)),
          }}
          settings={(at, header) => settingsFor(at, phoneSettingsGo, header, phoneSettingsReplace)}
          actions={phoneActions}
          filter={{ id: filterId, set: setProjectFilter }}
          filters={filterView}
          projectActions={projectActions}
          status={
            <>
              <StallBanner stalls={store.stalls} multiHost={store.hostList.length > 1} phone />
              <DeployBanner p={bannerDeploy(store)} threads={store.threads} conn={store.conn} phone onRestartNow={() => void restartNow()} />
            </>
          }
          overlay={
            <>
              {folderPickView}
              {confirmHost.view}
            </>
          }
        />
        </Boundary>
        </ProfilesContext.Provider>
        </ConfirmProvider>
        </HostMarksContext.Provider>
</HostsContext.Provider>
</UndContext.Provider>
        </FolderPickContext.Provider>
      </DirectoryContext.Provider>
    );
  }

  const activeSection: SectionKey | null = sel && store.threads[sel] ? sectionOf(store.threads[sel]) : null;
  const hints = keyHints({
    dialog,
    snoozeCount: dialog === "snooze" ? snoozeKeyCount(snoozeList()) : undefined,
    zone,
    hasSelection: !!sel,
    picked: picked.size,
    view: view?.mode ?? null,
    card: decision?.kind ?? null,
    cardMulti: !!(decision?.kind === "ask" && card && decision.questions?.[card.index]?.multiSelect),
    running: !!viewThread?.running,
    failed: (tgtThread(store, zone, view, sel)?.attention.state ?? "") === "failed",
    undo: !!toast?.undo && cmds.canUndo(),
    inText,
  });

  return (
    <div class="shell">
      <StallBanner stalls={store.stalls} multiHost={store.hostList.length > 1} />
      <DeployBanner p={bannerDeploy(store)} threads={store.threads} conn={store.conn} onRestartNow={() => void restartNow()} />
      {ctxMenu}
      <PreviewViewer />
      <DirectoryContext.Provider value={directory}>
      <FolderPickContext.Provider value={folderPicker}>
        <UndContext.Provider value={undNav}>
<HostsContext.Provider value={hostsCtx}>
<HostMarksContext.Provider value={store.hostMarks}>
      <ConfirmProvider value={confirmHost.ask}>
      <ProfilesContext.Provider value={byHost}>
      <div class={`${view ? "app open" : "app"}${listOn ? "" : " nolist"}`}>
        <Rail sec={sec} active={activeSection} profiles={byHost} conn={store.conn} onJump={jump} extra={pwaControls} listOn={listOn} onToggleList={toggleList} onSettings={() => setDialog("settings")} onChanges={() => setDialog("changes")} changesNeeds={needsCount(store.threads)} />
        <Boundary name="The inbox">
        <Inbox
          sec={sec}
          rows={rows}
          guest={guest}
          now={now}
          selected={sel}
          picked={picked}
          open={sectionOpen}
          expanded={expanded}
          query={query}
          searchRef={searchRef}
          searchable={searchList}
          onQuery={setQuery}
          onToggle={toggleSection}
          onSettleMerged={settleMerged}
          autoHideAt={hideShown}
          onSectionActivity={sectionUsed}
          onClick={(id) => openThread(id, "open")}
          onBackground={closeThread}
          onExpand={(k) => setExpanded((x) => ({ ...x, [k]: true }))}
          onNew={() => setDialog("new")}
          filter={filterView}
          unlock={unlock}
          quietHits={quietHits}
          extra={pwaControls}
          setup={
            <SetupInboxCard
              hosts={fullStore.hostList}
              projectCount={fullStore.projects.length}
              onOpen={() => {
                setSettingsAt({ page: "setup" });
                setDialog("settings");
              }}
            />
          }
          onFocusZone={() => setZone("list")}
          // V0.184: desktop only. The phone shows the list full screen with no pane beside it, so it has no edge to drag.
          resizer={<ListResizer win={winW} />}
        />
        </Boundary>
        {viewThread && view ? (
          <Boundary name="This thread" resetKey={view.id}>
          <ThreadPane
            thread={viewThread}
            mode={view.mode}
            events={store.events[view.id]}
            now={now}
            onEarlier={earlier[view.id] ? () => loadEarlier(view.id) : undefined}
            onBack={closePeek}
            profiles={byHost}
            card={card}
            answerField={answerField}
            onCard={(cs) => decision && setCards((c) => ({ ...c, [decision.id]: cs }))}
            onAllow={allow}
            onSubmitAsk={submitAsk}
            onCancelAsk={cancelAsk}
            arrived={arrivedHere ? { a: arrivedHere, onBack: () => (setArrival(null), openThread(arrivedHere.from, "open")), onDismiss: () => setArrival(null) } : undefined}
            onRun={(a, args) => {
              if (a === "thread.settle") return settle(view.id);
              if (a === "thread.archive") return archive([view.id]);
              void cmds.run(a, [view.id], args);
            }}
            onSnooze={() => openSnooze([view.id])}
            onAction={(a) => runDeskAction(a, view.id)}
            onSend={send}
            onAtEnd={(b) => setTracker((tr) => setAtEnd(tr, b))}
            onFocusZone={() => {
              setZone("thread");
              if (view.mode === "peek") openThread(view.id, "open");
            }}
          />
          </Boundary>
        ) : gatedThread ? (
          <section class="th empty-th">
            <QuietGate project={recordOf(store.projects, gatedThread.projectId)} now={Date.now() + store.clockOffset} unlock={unlock} what="thread" />
          </section>
        ) : store.loaded ? (
          // V0.status-home: no thread open: the status screen.
          <section class="th">
            <Boundary name="Status">
              <HomeScreen sec={sec} projects={store.projects} profiles={byHost} hosts={store.hostList} now={now} loaded={store.loaded} onOpen={(id) => openThread(id, "open")} onAllNeeds={() => jump("needs")} keys={HOME_KEYS} />
            </Boundary>
          </section>
        ) : (
          <section class="th empty-th">
            <div class="sys">{store.conn === "offline" ? "Fyr is not reachable. Retrying…" : "Connecting…"}</div>
          </section>
        )}
      </div>
      {help && (
        <footer class="keys" aria-label="Keys">
          {hints.map(([key, label]) => (
            <span class="kh">
              <kbd>{key}</kbd>
              {label}
            </span>
          ))}
          <button class="kh theme" onClick={cycleTheme} title="Theme">
            theme: {theme === "system" ? "auto" : theme}
          </button>
          <button class="kh details" onClick={() => setToolDetails(!toolDetailsOn)} aria-pressed={toolDetailsOn} title="Show tool details: every tool group open, every machine line in full">
            tool details: {toolDetailsOn ? "on" : "off"}
          </button>
          <span class="kh">
            <kbd>?</kbd>close
          </span>
        </footer>
      )}
      {toast && (
        <Pop kind="toast" class={`toast${toast.error ? " err" : ""}`} role="status">
          <span class="ell">{toast.text}</span>
          {toast.undo && cmds.canUndo() && (
            <button class="btn sm" onClick={() => void cmds.doUndo()}>
              Undo <kbd>⌘Z</kbd>
            </button>
          )}
        </Pop>
      )}
      {dialog === "new" && <NewThreadDialog onAddProject={projectActions.add} profiles={byHost} hosts={store.hostList} projects={store.projects} projectFilter={filterId} clockOffset={store.clockOffset} onClose={() => setDialog(null)} onCreate={create} onNewProject={(name, folder, host) => projectActions.create(name, folder, [], host)} />}
      {dialog === "picker" && (
        <Modal title={pickIds.length > 1 ? `Project for ${pickIds.length} threads` : "Project"} onClose={() => setDialog(null)} wide>
          <div class="db">
            <ProjectPicker
              projects={store.projects}
              threads={pickIds.map((id) => store.threads[id]).filter(Boolean)}
              onPick={async (p) => {
                const e = await projectActions.move(pickIds, p?.id ?? null);
                if (!e) setDialog(null);
              }}
              onCreate={async (name, folder) => {
                const r = await projectActions.create(name, folder, pickIds);
                if (!r.error) setDialog(null);
                return r.error;
              }}
              onEdit={(p) => p.id && openProjectScreen(p.id)}
            />
          </div>
        </Modal>
      )}
      {dialog === "handover" && handoverId && store.threads[handoverId] && (
        <HandoverDialog
          t={store.threads[handoverId]}
          profiles={profilesOn(rawStore, store.threads[handoverId].hostId)}
          projects={store.projects}
          onClose={() => setDialog(null)}
          onConfirm={(pr, m, e) => cmds.handover(handoverId, pr, m, e)}
          onConfirmHost={async (args) => {
            const r = await cmds.handoverHost(handoverId, args);
            if (r.error) return r.error;
            if (r.threadId) openThread(r.threadId, "open");
            return null;
          }}
        />
      )}
      {dialog === "changes" && (
        <Modal title="Changes" wide onClose={() => setDialog(null)}>
          <div class="db">
            <ChangesBoard
              threads={store.threads}
              projects={store.projects}
              onRun={(id, a, args) => void cmds.run(a, [id], args)}
              visit={api.boardVisit ? () => api.boardVisit!() : undefined}
              onOpenThread={(id) => {
                setDialog(null);
                openThread(id, "open");
              }}
            />
          </div>
        </Modal>
      )}
      {dialog === "settings" && (
        <Modal title="Settings" wide cls="settings" onClose={() => setDialog(null)}>
          {settingsFor(settingsAt, (r) => setSettingsAt(r ?? { page: "providers" }))}
        </Modal>
      )}
      {dialog === "snooze" && <SnoozeMenu presets={snoozeList()} count={snoozeTargets.length} onPick={snooze} onClose={() => setDialog(null)} />}
      </ProfilesContext.Provider>
      {folderPickView}
      {confirmHost.view}
      </ConfirmProvider>
      </HostMarksContext.Provider>
</HostsContext.Provider>
</UndContext.Provider>
        </FolderPickContext.Provider>
      </DirectoryContext.Provider>
    </div>
  );
}

function tgtThread(s: View, zone: Zone, view: { id: string } | null, sel: string | null): Thread | undefined {
  const id = zone === "thread" && view ? view.id : sel;
  return id ? s.threads[id] : undefined;
}

installPress();
render(
  <ApiContext.Provider value={hostApi}>
    <App />
  </ApiContext.Provider>,
  document.getElementById("app")!,
);
