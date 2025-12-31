export namespace main {
	
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

