// *** WARNING: this file was generated by the Lumi IDL Compiler (LUMIDL). ***
// *** Do not edit by hand unless you're certain you know what you are doing! ***

import * as lumi from "@lumi/lumi";

export let BinaryAttribute: AttributeType = "B";
export let NumberAttribute: AttributeType = "N";
export let StringAttribute: AttributeType = "S";

export interface Attribute {
    name: string;
    type: AttributeType;
}

export type AttributeType =
    "B" |
    "N" |
    "S";

export class Table extends lumi.Resource implements TableArgs {
    public readonly name: string;
    public readonly hashKey: string;
    public attributes: Attribute[];
    public readCapacity: number;
    public writeCapacity: number;
    public readonly rangeKey?: string;
    public readonly tableName?: string;

    constructor(name: string, args: TableArgs) {
        super();
        if (name === undefined) {
            throw new Error("Missing required resource name");
        }
        this.name = name;
        if (args.hashKey === undefined) {
            throw new Error("Missing required argument 'hashKey'");
        }
        this.hashKey = args.hashKey;
        if (args.attributes === undefined) {
            throw new Error("Missing required argument 'attributes'");
        }
        this.attributes = args.attributes;
        if (args.readCapacity === undefined) {
            throw new Error("Missing required argument 'readCapacity'");
        }
        this.readCapacity = args.readCapacity;
        if (args.writeCapacity === undefined) {
            throw new Error("Missing required argument 'writeCapacity'");
        }
        this.writeCapacity = args.writeCapacity;
        this.rangeKey = args.rangeKey;
        this.tableName = args.tableName;
    }
}

export interface TableArgs {
    readonly hashKey: string;
    attributes: Attribute[];
    readCapacity: number;
    writeCapacity: number;
    readonly rangeKey?: string;
    readonly tableName?: string;
}


