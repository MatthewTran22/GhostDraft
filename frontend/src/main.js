import './style.css';
import { GetConnectionStatus } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

// Initial HTML structure
document.querySelector('#app').innerHTML = `
    <div class="overlay-box" id="overlay-box">
        <div class="header drag-region">
            <h1>GhostDraft</h1>
        </div>

        <div class="status-card" id="status-card">
            <div class="status-indicator">
                <div class="status-dot waiting" id="status-dot"></div>
                <span class="status-message" id="status-message">Initializing...</span>
            </div>
        </div>

        <div class="tabs-container hidden" id="tabs-container">
            <div class="tabs-header">
                <button class="tab-btn active" data-tab="matchup">Matchup</button>
                <button class="tab-btn" data-tab="build">Build</button>
                <button class="tab-btn" data-tab="teamcomp">Team Comp</button>
            </div>

            <div class="tab-content active" id="tab-matchup">
                <div class="teamcomp-card hidden" id="teamcomp-card">
                    <div class="teamcomp-warning" id="teamcomp-warning"></div>
                </div>

                <div class="bans-card" id="bans-card">
                    <div class="bans-header">Recommended Bans</div>
                    <div class="bans-subheader" id="bans-subheader"></div>
                    <div class="bans-list" id="bans-list"></div>
                </div>

                <div class="build-card hidden" id="build-card">
                    <div class="build-role" id="build-role"></div>
                    <div class="build-matchup">
                        <span class="winrate-label" id="winrate-label">Win Rate</span>
                        <span class="build-winrate" id="build-winrate"></span>
                    </div>
                </div>
            </div>

            <div class="tab-content" id="tab-build">
                <div class="items-section" id="items-section">
                    <div class="items-header">Starting Items</div>
                    <div class="items-grid" id="starting-items"></div>
                    <div class="items-header">Core Build</div>
                    <div class="items-grid" id="core-items"></div>
                    <div class="items-header">4th Item Options</div>
                    <div class="items-grid" id="fourth-items"></div>
                    <div class="items-header">5th Item Options</div>
                    <div class="items-grid" id="fifth-items"></div>
                    <div class="items-header">6th Item Options</div>
                    <div class="items-grid" id="sixth-items"></div>
                </div>
            </div>

            <div class="tab-content" id="tab-teamcomp">
                <div class="comp-waiting" id="comp-waiting">Waiting for all players to lock in...</div>
                <div class="comp-analysis hidden" id="comp-analysis">
                    <div class="comp-section">
                        <div class="comp-section-header ally">Your Team</div>
                        <div class="comp-archetype" id="ally-archetype"></div>
                        <div class="comp-tags" id="ally-tags"></div>
                        <div class="comp-damage" id="ally-damage"></div>
                    </div>
                    <div class="comp-section">
                        <div class="comp-section-header enemy">Enemy Team</div>
                        <div class="comp-archetype" id="enemy-archetype"></div>
                        <div class="comp-tags" id="enemy-tags"></div>
                        <div class="comp-damage" id="enemy-damage"></div>
                    </div>
                </div>
            </div>
        </div>
    </div>
`;

// DOM elements
const statusDot = document.getElementById('status-dot');
const statusMessage = document.getElementById('status-message');
const statusCard = document.getElementById('status-card');
const tabsContainer = document.getElementById('tabs-container');
const teamcompCard = document.getElementById('teamcomp-card');
const teamcompWarning = document.getElementById('teamcomp-warning');
const bansCard = document.getElementById('bans-card');
const bansSubheader = document.getElementById('bans-subheader');
const bansList = document.getElementById('bans-list');
const buildCard = document.getElementById('build-card');
const buildRole = document.getElementById('build-role');
const buildWinrate = document.getElementById('build-winrate');
const winrateLabel = document.getElementById('winrate-label');
const compWaiting = document.getElementById('comp-waiting');
const compAnalysis = document.getElementById('comp-analysis');
const startingItemsGrid = document.getElementById('starting-items');
const coreItemsGrid = document.getElementById('core-items');
const fourthItemsGrid = document.getElementById('fourth-items');
const fifthItemsGrid = document.getElementById('fifth-items');
const sixthItemsGrid = document.getElementById('sixth-items');
const allyArchetype = document.getElementById('ally-archetype');
const allyTags = document.getElementById('ally-tags');
const allyDamage = document.getElementById('ally-damage');
const enemyArchetype = document.getElementById('enemy-archetype');
const enemyTags = document.getElementById('enemy-tags');
const enemyDamage = document.getElementById('enemy-damage');

// Tab switching
document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.addEventListener('click', () => {
        // Update active button
        document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        // Update active content
        document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
        document.getElementById(`tab-${btn.dataset.tab}`).classList.add('active');
    });
});

// Format role name for display
function formatRole(role) {
    const roleMap = {
        'utility': 'Support',
        'middle': 'Mid',
        'bottom': 'ADC',
        'jungle': 'Jungle',
        'top': 'Top'
    };
    return roleMap[role] || role || '';
}

// Update connection status
function updateStatus(status) {
    statusMessage.textContent = status.message;
    statusDot.className = status.connected ? 'status-dot connected' : 'status-dot waiting';
}

// Update champ select state
function updateChampSelect(data) {
    if (!data.inChampSelect) {
        tabsContainer.classList.add('hidden');
        statusCard.classList.remove('hidden');
        return;
    }
    // In champ select - show tabs
    statusCard.classList.add('hidden');
    tabsContainer.classList.remove('hidden');
}

// Update team comp warning
function updateTeamComp(data) {
    if (!data || !data.show) {
        teamcompCard.classList.add('hidden');
        return;
    }

    teamcompCard.classList.remove('hidden');
    teamcompCard.className = `teamcomp-card ${data.severity}`;
    teamcompWarning.textContent = data.recommendation;
}

// Update recommended bans
function updateBans(data) {
    if (!data || !data.hasBans) {
        bansSubheader.textContent = '';
        bansList.innerHTML = '';
        return;
    }

    // No matchup data for this pick
    if (data.noData || !data.bans || data.bans.length === 0) {
        bansSubheader.textContent = `${data.championName} ${formatRole(data.role)}`;
        bansList.innerHTML = `<div class="no-data-msg">WTF kind of pick is this?</div>`;
        return;
    }

    // Update header
    bansSubheader.textContent = `Counters for ${data.championName} (${formatRole(data.role)})`;

    // Build ban list HTML
    let html = '';
    for (let i = 0; i < data.bans.length; i++) {
        const ban = data.bans[i];
        const wr = typeof ban.winRate === 'number' ? ban.winRate.toFixed(1) : ban.winRate;
        const dmgClass = ban.damageType === 'AP' ? 'ap' : ban.damageType === 'AD' ? 'ad' : 'mixed';
        html += `
            <div class="ban-row">
                <span class="ban-rank">${i + 1}</span>
                <img class="ban-icon" src="${ban.iconURL}" alt="${ban.championName}" />
                <span class="ban-name">${ban.championName}</span>
                <span class="ban-dmg ${dmgClass}">${ban.damageType}</span>
                <span class="ban-wr losing">${wr}%</span>
            </div>
        `;
    }
    bansList.innerHTML = html;
}

// Update build/matchup data
function updateBuild(data) {
    if (!data.hasBuild) {
        buildCard.classList.add('hidden');
        return;
    }

    buildCard.classList.remove('hidden');

    buildRole.textContent = formatRole(data.role);
    winrateLabel.textContent = data.winRateLabel || 'Win Rate';
    buildWinrate.textContent = data.winRate;

    buildWinrate.classList.remove('winning', 'losing', 'even');
    if (data.matchupStatus) {
        buildWinrate.classList.add(data.matchupStatus);
    }
}

// Update full team comp analysis (when all locked in)
function updateFullComp(data) {
    if (!data || !data.ready) {
        compWaiting.classList.remove('hidden');
        compAnalysis.classList.add('hidden');
        return;
    }

    compWaiting.classList.add('hidden');
    compAnalysis.classList.remove('hidden');

    // Render ally team
    allyArchetype.textContent = data.allyArchetype;
    allyTags.innerHTML = (data.allyTags || []).map(tag =>
        `<span class="comp-tag">${tag}</span>`
    ).join('') || '<span class="comp-tag-none">No dominant tags</span>';
    allyDamage.innerHTML = `
        <span class="dmg-bar">
            <span class="dmg-ap" style="width: ${data.allyAP}%">${data.allyAP}% AP</span>
            <span class="dmg-ad" style="width: ${data.allyAD}%">${data.allyAD}% AD</span>
        </span>
    `;

    // Render enemy team
    enemyArchetype.textContent = data.enemyArchetype;
    enemyTags.innerHTML = (data.enemyTags || []).map(tag =>
        `<span class="comp-tag">${tag}</span>`
    ).join('') || '<span class="comp-tag-none">No dominant tags</span>';
    enemyDamage.innerHTML = `
        <span class="dmg-bar">
            <span class="dmg-ap" style="width: ${data.enemyAP}%">${data.enemyAP}% AP</span>
            <span class="dmg-ad" style="width: ${data.enemyAD}%">${data.enemyAD}% AD</span>
        </span>
    `;
}

// Update item build
function updateItems(data) {
    if (!data || !data.hasItems) {
        startingItemsGrid.innerHTML = '<div class="items-empty">Select a champion...</div>';
        coreItemsGrid.innerHTML = '';
        fourthItemsGrid.innerHTML = '';
        fifthItemsGrid.innerHTML = '';
        sixthItemsGrid.innerHTML = '';
        return;
    }

    // Helper to render item grid
    const renderItems = (items, grid) => {
        if (items && items.length > 0) {
            grid.innerHTML = items.map(item => `
                <div class="item-slot">
                    <img class="item-icon" src="${item.iconURL}" alt="${item.name}" title="${item.name}" />
                </div>
            `).join('');
        } else {
            grid.innerHTML = '<div class="items-empty">No data</div>';
        }
    };

    renderItems(data.startingItems, startingItemsGrid);
    renderItems(data.coreItems, coreItemsGrid);
    renderItems(data.fourthItems, fourthItemsGrid);
    renderItems(data.fifthItems, fifthItemsGrid);
    renderItems(data.sixthItems, sixthItemsGrid);
}

// Event listeners
EventsOn('lcu:status', updateStatus);
EventsOn('champselect:update', updateChampSelect);
EventsOn('build:update', updateBuild);
EventsOn('bans:update', updateBans);
EventsOn('teamcomp:update', updateTeamComp);
EventsOn('fullcomp:update', updateFullComp);
EventsOn('items:update', updateItems);

// Get initial status
GetConnectionStatus()
    .then(updateStatus)
    .catch(() => {
        updateStatus({ connected: false, message: 'Waiting for League...' });
    });
