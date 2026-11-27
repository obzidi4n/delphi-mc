package dev.delphi.bridge;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import org.bukkit.BanList;
import org.bukkit.Bukkit;
import org.bukkit.entity.Player;
import org.bukkit.event.EventHandler;
import org.bukkit.event.EventPriority;
import org.bukkit.event.Listener;
import io.papermc.paper.event.player.AsyncChatEvent;
import net.kyori.adventure.text.serializer.plain.PlainTextComponentSerializer;
import org.bukkit.event.player.PlayerJoinEvent;
import org.bukkit.event.player.PlayerQuitEvent;
import org.bukkit.plugin.java.JavaPlugin;

import java.io.IOException;
import java.io.Reader;
import java.io.Writer;
import java.nio.charset.StandardCharsets;
import java.nio.file.AtomicMoveNotSupportedException;
import java.nio.file.DirectoryStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.Deque;
import java.util.List;
import java.util.Set;
import java.util.concurrent.ConcurrentLinkedDeque;

/**
 * File-based bridge: the agent and plugin share the server directory.
 * The plugin publishes state.json (atomic replace) and consumes cmd-*.json.
 */
public final class DelphiBridge extends JavaPlugin implements Listener {
    private static final int EVENT_CAP = 200;
    private static final int COMMAND_CAP = 500;

    private final Gson gson = new GsonBuilder().disableHtmlEscaping().create();
    private final Deque<Event> events = new ConcurrentLinkedDeque<>();
    private Path cmdDir;
    private Path stateFile;
    private long seq = 0;
    private volatile double tps = 20.0;
    private volatile List<String> commands = Collections.emptyList();

    public static final class Event {
        long seq;
        String type;
        String player;
        String message;
        String action;
        Boolean ok;
        String error;
        String id;
    }

    public static final class PlayerInfo {
        String name;
        String uuid;
    }

    public static final class State {
        int version = 1;
        long seq;
        double tps;
        List<PlayerInfo> players = new ArrayList<>();
        List<String> commands = new ArrayList<>();
        List<Event> events = new ArrayList<>();
        long at;
    }

    private static final class Command {
        String id;
        String action;
        String player;
        String reason;
    }

    @Override
    public void onEnable() {
        Path root = getServer().getWorldContainer().toPath().toAbsolutePath();
        Path bridgeDir = root.resolve(".delphi-bridge");
        cmdDir = bridgeDir.resolve("cmd");
        stateFile = bridgeDir.resolve("state.json");
        try {
            Files.createDirectories(cmdDir);
        } catch (IOException e) {
            getLogger().severe("cannot create bridge directory: " + e.getMessage());
        }
        getServer().getPluginManager().registerEvents(this, this);
        commands = knownCommands();
        record("ready", null, null, null, true, null, null);
        Bukkit.getScheduler().runTaskTimerAsynchronously(this, this::tick, 20L, 10L);
        getLogger().info("DelphiBridge enabled (bridge dir " + bridgeDir + ")");
    }

    private List<String> knownCommands() {
        try {
            Set<String> names = getServer().getCommandMap().getKnownCommands().keySet();
            List<String> out = new ArrayList<>();
            for (String name : names) {
                if (name.indexOf(':') >= 0) {
                    continue;
                }
                out.add(name);
                if (out.size() >= COMMAND_CAP) {
                    break;
                }
            }
            Collections.sort(out);
            return out;
        } catch (Throwable t) {
            getLogger().warning("command tree unavailable: " + t.getMessage());
            return Collections.emptyList();
        }
    }

    private void tick() {
        processCommands();
        double[] sample = Bukkit.getTPS();
        if (sample.length > 0) {
            tps = Math.round(sample[0] * 100.0) / 100.0;
        }
        List<PlayerInfo> players = new ArrayList<>();
        for (Player player : Bukkit.getOnlinePlayers()) {
            PlayerInfo info = new PlayerInfo();
            info.name = player.getName();
            info.uuid = player.getUniqueId().toString();
            players.add(info);
        }
        players.sort(Comparator.comparing(info -> info.name));

        List<Event> snapshot = new ArrayList<>(events);
        snapshot.sort(Comparator.comparingLong(e -> e.seq));

        State state = new State();
        state.seq = seq;
        state.tps = tps;
        state.players = players;
        state.commands = commands;
        state.events = snapshot;
        state.at = System.currentTimeMillis();
        writeState(state);
    }

    private void writeState(State state) {
        try {
            Path tmp = stateFile.resolveSibling("state.json.tmp");
            try (Writer writer = Files.newBufferedWriter(tmp, StandardCharsets.UTF_8)) {
                gson.toJson(state, writer);
            }
            try {
                Files.move(tmp, stateFile, StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.ATOMIC_MOVE);
            } catch (AtomicMoveNotSupportedException e) {
                Files.move(tmp, stateFile, StandardCopyOption.REPLACE_EXISTING);
            }
        } catch (IOException e) {
            getLogger().warning("cannot write bridge state: " + e.getMessage());
        }
    }

    private void processCommands() {
        if (cmdDir == null) {
            return;
        }
        try (DirectoryStream<Path> stream = Files.newDirectoryStream(cmdDir, "cmd-*.json")) {
            for (Path path : stream) {
                Command command = null;
                try (Reader reader = Files.newBufferedReader(path, StandardCharsets.UTF_8)) {
                    command = gson.fromJson(reader, Command.class);
                } catch (IOException e) {
                    getLogger().warning("cannot read command " + path.getFileName() + ": " + e.getMessage());
                }
                try {
                    Files.deleteIfExists(path);
                } catch (IOException e) {
                    getLogger().warning("cannot delete command " + path.getFileName() + ": " + e.getMessage());
                }
                if (command == null || command.action == null) {
                    continue;
                }
                final Command run = command;
                Bukkit.getScheduler().runTask(this, () -> execute(run));
            }
        } catch (IOException e) {
            getLogger().warning("cannot scan commands: " + e.getMessage());
        }
    }

    @SuppressWarnings({"deprecation", "unchecked"})
    private void execute(Command command) {
        boolean ok = true;
        String error = null;
        String reason = command.reason == null ? "" : command.reason;
        switch (command.action) {
            case "kick": {
                Player target = Bukkit.getPlayerExact(command.player == null ? "" : command.player);
                if (target == null) {
                    ok = false;
                    error = "player is not online";
                } else {
                    target.kickPlayer(reason.isEmpty() ? "Kicked by an operator" : reason);
                }
                break;
            }
            case "ban": {
                if (command.player == null || command.player.isBlank()) {
                    ok = false;
                    error = "player is required";
                    break;
                }
                BanList<Player> bans = (BanList<Player>) getServer().getBanList(BanList.Type.NAME);
                bans.addBan(command.player, reason.isEmpty() ? "Banned by an operator" : reason, (java.util.Date) null, "DelphiMC");
                Player online = Bukkit.getPlayerExact(command.player);
                if (online != null) {
                    online.kickPlayer(reason.isEmpty() ? "Banned by an operator" : reason);
                }
                break;
            }
            default:
                ok = false;
                error = "unknown action: " + command.action;
        }
        record("moderation", command.player, null, command.action, ok, error, command.id);
    }

    private void record(String type, String player, String message, String action,
                        Boolean ok, String error, String id) {
        Event event = new Event();
        event.seq = ++seq;
        event.type = type;
        event.player = player;
        event.message = message;
        event.action = action;
        event.ok = ok;
        event.error = error;
        event.id = id;
        events.addLast(event);
        while (events.size() > EVENT_CAP) {
            events.removeFirst();
        }
    }

    @EventHandler(priority = EventPriority.MONITOR, ignoreCancelled = true)
    public void onChat(AsyncChatEvent event) {
        String message = PlainTextComponentSerializer.plainText().serialize(event.message());
        record("chat", event.getPlayer().getName(), message, null, null, null, null);
    }

    @EventHandler(priority = EventPriority.MONITOR)
    public void onJoin(PlayerJoinEvent event) {
        record("join", event.getPlayer().getName(), null, null, null, null, null);
    }

    @EventHandler(priority = EventPriority.MONITOR)
    public void onQuit(PlayerQuitEvent event) {
        record("quit", event.getPlayer().getName(), null, null, null, null, null);
    }
}
