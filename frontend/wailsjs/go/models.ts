export namespace lcu {
	
	export class ChampionPersonalStats {
	    championId: number;
	    championName: string;
	    iconURL: string;
	    splashURL: string;
	    role: string;
	    roleIconURL: string;
	    games: number;
	    wins: number;
	    winRate: number;
	    avgKills: number;
	    avgDeaths: number;
	    avgAssists: number;
	    avgKDA: number;
	    avgCS: number;
	    avgCSPerMin: number;
	
	    static createFrom(source: any = {}) {
	        return new ChampionPersonalStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.championId = source["championId"];
	        this.championName = source["championName"];
	        this.iconURL = source["iconURL"];
	        this.splashURL = source["splashURL"];
	        this.role = source["role"];
	        this.roleIconURL = source["roleIconURL"];
	        this.games = source["games"];
	        this.wins = source["wins"];
	        this.winRate = source["winRate"];
	        this.avgKills = source["avgKills"];
	        this.avgDeaths = source["avgDeaths"];
	        this.avgAssists = source["avgAssists"];
	        this.avgKDA = source["avgKDA"];
	        this.avgCS = source["avgCS"];
	        this.avgCSPerMin = source["avgCSPerMin"];
	    }
	}
	export class PersonalStats {
	    hasData: boolean;
	    totalGames: number;
	    wins: number;
	    losses: number;
	    winRate: number;
	    avgKills: number;
	    avgDeaths: number;
	    avgAssists: number;
	    avgKDA: number;
	    avgCS: number;
	    avgCSPerMin: number;
	    championStats: ChampionPersonalStats[];
	
	    static createFrom(source: any = {}) {
	        return new PersonalStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hasData = source["hasData"];
	        this.totalGames = source["totalGames"];
	        this.wins = source["wins"];
	        this.losses = source["losses"];
	        this.winRate = source["winRate"];
	        this.avgKills = source["avgKills"];
	        this.avgDeaths = source["avgDeaths"];
	        this.avgAssists = source["avgAssists"];
	        this.avgKDA = source["avgKDA"];
	        this.avgCS = source["avgCS"];
	        this.avgCSPerMin = source["avgCSPerMin"];
	        this.championStats = this.convertValues(source["championStats"], ChampionPersonalStats);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class BuildItem {
	    id: number;
	    name: string;
	    iconURL: string;
	    winRate?: number;
	    games?: number;
	
	    static createFrom(source: any = {}) {
	        return new BuildItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.iconURL = source["iconURL"];
	        this.winRate = source["winRate"];
	        this.games = source["games"];
	    }
	}
	export class BuildPath {
	    name: string;
	    winRate: number;
	    games: number;
	    startingItems: BuildItem[];
	    coreItems: BuildItem[];
	    fourthItems: BuildItem[];
	    fifthItems: BuildItem[];
	    sixthItems: BuildItem[];
	
	    static createFrom(source: any = {}) {
	        return new BuildPath(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.winRate = source["winRate"];
	        this.games = source["games"];
	        this.startingItems = this.convertValues(source["startingItems"], BuildItem);
	        this.coreItems = this.convertValues(source["coreItems"], BuildItem);
	        this.fourthItems = this.convertValues(source["fourthItems"], BuildItem);
	        this.fifthItems = this.convertValues(source["fifthItems"], BuildItem);
	        this.sixthItems = this.convertValues(source["sixthItems"], BuildItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ChampionBuildData {
	    hasItems: boolean;
	    championName: string;
	    role: string;
	    builds: BuildPath[];
	
	    static createFrom(source: any = {}) {
	        return new ChampionBuildData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hasItems = source["hasItems"];
	        this.championName = source["championName"];
	        this.role = source["role"];
	        this.builds = this.convertValues(source["builds"], BuildPath);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ChampionDetailItem {
	    itemId: number;
	    name: string;
	    iconURL: string;
	    winRate: number;
	    games: number;
	
	    static createFrom(source: any = {}) {
	        return new ChampionDetailItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.itemId = source["itemId"];
	        this.name = source["name"];
	        this.iconURL = source["iconURL"];
	        this.winRate = source["winRate"];
	        this.games = source["games"];
	    }
	}
	export class ChampionDetailMatchup {
	    championId: number;
	    championName: string;
	    iconURL: string;
	    winRate: number;
	    games: number;
	
	    static createFrom(source: any = {}) {
	        return new ChampionDetailMatchup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.championId = source["championId"];
	        this.championName = source["championName"];
	        this.iconURL = source["iconURL"];
	        this.winRate = source["winRate"];
	        this.games = source["games"];
	    }
	}
	export class ChampionDetails {
	    hasData: boolean;
	    championId: number;
	    championName: string;
	    role: string;
	    coreItems: ChampionDetailItem[];
	    fourthItems: ChampionDetailItem[];
	    fifthItems: ChampionDetailItem[];
	    sixthItems: ChampionDetailItem[];
	    counters: ChampionDetailMatchup[];
	    goodMatchups: ChampionDetailMatchup[];
	
	    static createFrom(source: any = {}) {
	        return new ChampionDetails(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hasData = source["hasData"];
	        this.championId = source["championId"];
	        this.championName = source["championName"];
	        this.role = source["role"];
	        this.coreItems = this.convertValues(source["coreItems"], ChampionDetailItem);
	        this.fourthItems = this.convertValues(source["fourthItems"], ChampionDetailItem);
	        this.fifthItems = this.convertValues(source["fifthItems"], ChampionDetailItem);
	        this.sixthItems = this.convertValues(source["sixthItems"], ChampionDetailItem);
	        this.counters = this.convertValues(source["counters"], ChampionDetailMatchup);
	        this.goodMatchups = this.convertValues(source["goodMatchups"], ChampionDetailMatchup);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MetaChampion {
	    championId: number;
	    championName: string;
	    iconURL: string;
	    winRate: number;
	    pickRate: number;
	    games: number;
	
	    static createFrom(source: any = {}) {
	        return new MetaChampion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.championId = source["championId"];
	        this.championName = source["championName"];
	        this.iconURL = source["iconURL"];
	        this.winRate = source["winRate"];
	        this.pickRate = source["pickRate"];
	        this.games = source["games"];
	    }
	}
	export class MetaData {
	    patch: string;
	    hasData: boolean;
	    roles: Record<string, Array<MetaChampion>>;
	
	    static createFrom(source: any = {}) {
	        return new MetaData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.patch = source["patch"];
	        this.hasData = source["hasData"];
	        this.roles = this.convertValues(source["roles"], Array<MetaChampion>, true);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

